package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowPostgresStore is the PG-backed IFlowInfoStore implementation.
type FlowPostgresStore struct {
	inner *store.PostgresGenericStore[entities.FlowVersionInfo]
}

func NewFlowPostgresStore(pool *pgxpool.Pool) *FlowPostgresStore {
	return &FlowPostgresStore{
		inner: &store.PostgresGenericStore[entities.FlowVersionInfo]{
			Pool: pool, Table: "orh_agentflow", IDCol: "agentflow_id",
		},
	}
}

func (s *FlowPostgresStore) Get(ctx context.Context, namespace, id string) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=$1 AND agentflow_id=$2 AND del_flag=false ORDER BY version DESC LIMIT 1", namespace, id)
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
	if err := s.inner.Pool.QueryRow(ctx,
		"SELECT COUNT(1) FROM orh_agentflow WHERE namespace_id=$1 AND del_flag=false", namespace).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=$1 AND del_flag=false ORDER BY created_at DESC LIMIT $2 OFFSET $3",
		namespace, req.Size, offset)
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
	_, err := s.inner.Pool.Exec(ctx, "UPDATE orh_agentflow SET status='DELETED',del_flag=true,updated_at=NOW() WHERE namespace_id=$1 AND agentflow_id=$2", namespace, id)
	return err
}

func (s *FlowPostgresStore) GetVersion(ctx context.Context, namespace, id string, ver int64) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=$1 AND agentflow_id=$2 AND version=$3 AND del_flag=false", namespace, id, ver)
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
	_, err = s.inner.Pool.Exec(ctx,
		`INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,namespace_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (namespace_id,agentflow_id,version) DO UPDATE SET
		   definition=$4,comment=$6,status='ACTIVE',del_flag=false,updated_at=NOW()`,
		uuid.New().String(), spec.ID, int64(1), b, createdBy, comment, spec.Namespace)
	return err
}

func (s *FlowPostgresStore) CreateSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	b, err := marshalSpec(spec)
	if err != nil {
		return err
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
