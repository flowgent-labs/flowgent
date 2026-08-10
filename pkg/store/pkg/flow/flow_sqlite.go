package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
)

type FlowSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.FlowVersionInfo]
}

func NewFlowSQLiteStore(conn *sql.DB) *FlowSQLiteStore {
	return &FlowSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.FlowVersionInfo]{
			Conn: conn, Table: "orh_agentflow", IDCol: "agentflow_id",
		},
	}
}
func (s *FlowSQLiteStore) Get(ctx context.Context, id string) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	row := s.inner.Conn.QueryRowContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE agentflow_id=?1 AND del_flag=0 ORDER BY version DESC LIMIT 1", id)
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}

	var total int64
	if err := s.inner.Conn.QueryRowContext(ctx,
		"SELECT COUNT(1) FROM orh_agentflow WHERE del_flag=0").Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size
	rows, err := s.inner.Conn.QueryContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE del_flag=0 ORDER BY created_at DESC LIMIT ?1 OFFSET ?2",
		req.Size, offset)
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
func (s *FlowSQLiteStore) Save(ctx context.Context, e *entities.FlowVersionInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
func (s *FlowSQLiteStore) GetVersion(ctx context.Context, id string, ver int64) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	row := s.inner.Conn.QueryRowContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE agentflow_id=?1 AND version=?2 AND del_flag=0", id, ver)
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowSQLiteStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	b, _ := json.Marshal(spec)
	_, err := s.inner.Conn.ExecContext(ctx,
		"INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,priority,namespace_id) VALUES (?1,?2,?3,?4,?5,?6,?7,?8) ON CONFLICT (agentflow_id,version) DO UPDATE SET definition=?4,comment=?6,priority=?7,namespace_id=?8,status='ACTIVE',del_flag=0,updated_at=CURRENT_TIMESTAMP",
		uuid.New().String(), spec.ID, int64(1), b, createdBy, comment, string(spec.Priority), spec.Namespace)
	return err
}
func (s *FlowSQLiteStore) GetSpec(ctx context.Context, id string) (*entities.FlowInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	row := s.inner.Conn.QueryRowContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE agentflow_id=?1 AND del_flag=0 ORDER BY version DESC LIMIT 1", id)
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &v); err != nil {
		return nil, err
	}
	var spec entities.FlowInfo
	if err := json.Unmarshal(v.Definition, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}
