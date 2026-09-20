package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"modernc.org/sqlite"
)

type FlowSQLiteStore struct {
	inner *storage.SQLiteGenericStore[entities.FlowVersionInfo]
}

func NewFlowSQLiteStore(conn *sql.DB) *FlowSQLiteStore {
	return &FlowSQLiteStore{
		inner: &storage.SQLiteGenericStore[entities.FlowVersionInfo]{
			Conn: conn, Table: "orh_agentflow", IDCol: "agentflow_id",
		},
	}
}
func (s *FlowSQLiteStore) Get(ctx context.Context, namespace, id string) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).SQLiteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	row := s.inner.Conn.QueryRowContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=?1 AND agentflow_id=?2 AND del_flag=0 AND ("+scopeWhere+") ORDER BY version DESC LIMIT 1", args...)
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowSQLiteStore) Select(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}

	var total int64
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).SQLiteWhere()
	countArgs := append([]any{namespace}, scopeArgs...)
	if err := s.inner.Conn.QueryRowContext(ctx,
		"SELECT COUNT(1) FROM orh_agentflow WHERE namespace_id=?1 AND del_flag=0 AND ("+scopeWhere+")", countArgs...).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size
	queryArgs := append(countArgs, req.Size, offset)
	rows, err := s.inner.Conn.QueryContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=?1 AND del_flag=0 AND ("+scopeWhere+") ORDER BY created_at DESC LIMIT ? OFFSET ?",
		queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*entities.FlowVersionInfo
	for rows.Next() {
		e := new(entities.FlowVersionInfo)
		if err := utils.ScanStruct(rows, e); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		items = append(items, e)
	}
	return entities.NewPage(items, total, req), nil
}
func (s *FlowSQLiteStore) Delete(ctx context.Context, namespace, id string) error {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).SQLiteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	_, err := s.inner.Conn.ExecContext(ctx, "UPDATE orh_agentflow SET status='DELETED',del_flag=1,updated_at=CURRENT_TIMESTAMP WHERE namespace_id=?1 AND agentflow_id=?2 AND ("+scopeWhere+")", args...)
	return err
}
func (s *FlowSQLiteStore) GetVersion(ctx context.Context, namespace, id string, ver int64) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).SQLiteWhere()
	args := append([]any{namespace, id, ver}, scopeArgs...)
	row := s.inner.Conn.QueryRowContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE namespace_id=?1 AND agentflow_id=?2 AND version=?3 AND del_flag=0 AND ("+scopeWhere+")", args...)
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowSQLiteStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
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
	_, err = s.inner.Conn.ExecContext(ctx,
		"INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,namespace_id) VALUES (?1,?2,?3,?4,?5,?6,?7) ON CONFLICT (namespace_id,agentflow_id,version) DO UPDATE SET definition=?4,comment=?6,status='ACTIVE',del_flag=0,updated_at=CURRENT_TIMESTAMP",
		uuid.New().String(), spec.ID, int64(1), b, createdBy, comment, spec.Namespace)
	return err
}

func (s *FlowSQLiteStore) CreateSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
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
	_, err = s.inner.Conn.ExecContext(ctx,
		"INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,namespace_id) VALUES (?1,?2,?3,?4,?5,?6,?7)",
		uuid.New().String(), spec.ID, int64(1), b, createdBy, comment, spec.Namespace)
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == 19 {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, spec.ID)
	}
	return err
}

func (s *FlowSQLiteStore) isCandidateVisible(ctx context.Context, namespace, flowID string) (bool, error) {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).SQLiteWhere()
	args := append([]any{namespace, flowID}, scopeArgs...)
	var visible int
	err := s.inner.Conn.QueryRowContext(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST(? AS TEXT) AS namespace_id, CAST(? AS TEXT) AS agentflow_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *FlowSQLiteStore) GetSpec(ctx context.Context, namespace, id string) (*entities.FlowInfo, error) {
	v, err := s.Get(ctx, namespace, id)
	if err != nil {
		return nil, err
	}
	var spec entities.FlowInfo
	if err := json.Unmarshal(v.Definition, &spec); err != nil {
		return nil, err
	}
	hydrateSpec(&spec, v)
	return &spec, nil
}
