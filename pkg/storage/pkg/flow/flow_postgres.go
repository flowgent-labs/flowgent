package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowPostgresStore is the PG-backed IFlowInfoStore implementation.
type FlowPostgresStore struct {
	inner *storage.PostgresGenericStore[entities.FlowVersionInfo]
}

func NewFlowPostgresStore(pool *pgxpool.Pool) *FlowPostgresStore {
	return &FlowPostgresStore{
		inner: &storage.PostgresGenericStore[entities.FlowVersionInfo]{
			Pool: pool, Table: "orh_agentflow", IDCol: "agentflow_id",
		},
	}
}

func (s *FlowPostgresStore) Get(ctx context.Context, namespace, id string) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=$1 AND agentflow_id=$2 AND del_flag=false AND ("+scopeWhere+") ORDER BY version DESC LIMIT 1", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("not found")
	}
	var ver entities.FlowVersionInfo
	if err := utils.ScanStruct(rows, &ver); err != nil {
		return nil, err
	}
	return &ver, nil
}
func (s *FlowPostgresStore) Select(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}

	var total int64
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(2)
	countArgs := append([]any{namespace}, scopeArgs...)
	if err := s.inner.Pool.QueryRow(ctx,
		"SELECT COUNT(DISTINCT agentflow_id) FROM orh_agentflow WHERE namespace_id=$1 AND del_flag=false AND ("+scopeWhere+")", countArgs...).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size
	limitParameter := len(countArgs) + 1
	queryArgs := append(countArgs, req.Size, offset)
	rows, err := s.inner.Pool.Query(ctx,
		fmt.Sprintf("SELECT DISTINCT ON (agentflow_id) %s FROM orh_agentflow WHERE namespace_id=$1 AND del_flag=false AND (%s) ORDER BY agentflow_id, version DESC LIMIT $%d OFFSET $%d", cols, scopeWhere, limitParameter, limitParameter+1),
		queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*entities.FlowVersionInfo
	for rows.Next() {
		entity := new(entities.FlowVersionInfo)
		if err := utils.ScanStruct(rows, entity); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		items = append(items, entity)
	}
	return entities.NewPage(items, total, req), nil
}
func (s *FlowPostgresStore) Delete(ctx context.Context, namespace, id string) error {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	_, err := s.inner.Pool.Exec(ctx, "UPDATE orh_agentflow SET status='DELETED',del_flag=true,updated_at=NOW() WHERE namespace_id=$1 AND agentflow_id=$2 AND ("+scopeWhere+")", args...)
	return err
}

func (s *FlowPostgresStore) GetVersion(ctx context.Context, namespace, id string, ver int64) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(4)
	args := append([]any{namespace, id, ver}, scopeArgs...)
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=$1 AND agentflow_id=$2 AND version=$3 AND del_flag=false AND ("+scopeWhere+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("not found")
	}
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(rows, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowPostgresStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	b, err := marshalSpec(spec)
	if err != nil {
		return err
	}
	visible, err := s.isCandidateVisible(ctx, spec.Namespace, spec.ID)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.inner.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	versionArgs := append([]any{spec.Namespace, spec.ID}, scopeArgs...)
	var nextVersion int64
	if err := tx.QueryRow(ctx,
		"SELECT COALESCE(MAX(version), 0) + 1 FROM orh_agentflow WHERE namespace_id=$1 AND agentflow_id=$2 AND ("+scopeWhere+")",
		versionArgs...).Scan(&nextVersion); err != nil {
		return err
	}
	spec.Version = nextVersion
	_, err = tx.Exec(ctx,
		`INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,namespace_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uuid.New().String(), spec.ID, nextVersion, b, createdBy, comment, spec.Namespace)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *FlowPostgresStore) CreateSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	b, err := marshalSpec(spec)
	if err != nil {
		return err
	}
	visible, err := s.isCandidateVisible(ctx, spec.Namespace, spec.ID)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	_, err = s.inner.Pool.Exec(ctx,
		`INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,namespace_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uuid.New().String(), spec.ID, int64(1), b, createdBy, comment, spec.Namespace)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, spec.ID)
	}
	return err
}

func (s *FlowPostgresStore) isCandidateVisible(ctx context.Context, namespace, flowID string) (bool, error) {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	args := append([]any{namespace, flowID}, scopeArgs...)
	var visible int
	err := s.inner.Pool.QueryRow(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST($1 AS TEXT) AS namespace_id, CAST($2 AS TEXT) AS agentflow_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *FlowPostgresStore) GetSpec(ctx context.Context, namespace, id string) (*entities.FlowInfo, error) {
	ver, err := s.Get(ctx, namespace, id)
	if err != nil {
		return nil, err
	}
	var spec entities.FlowInfo
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, err
	}
	hydrateSpec(&spec, ver)
	return &spec, nil
}
