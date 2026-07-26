package flow

import (
	"context"
	"database/sql"
	"encoding/json"

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
	return s.inner.Get(ctx, id)
}
func (s *FlowSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	return s.inner.Select(ctx, req)
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
		"SELECT "+cols+" FROM orh_agentflow WHERE agentflow_id=?1 AND version=?2", id, ver)
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowSQLiteStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	b, _ := json.Marshal(spec)
	var nextVer int64
	_ = s.inner.Conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM orh_agentflow WHERE agentflow_id=?1", spec.ID).Scan(&nextVer)
	if nextVer == 0 {
		nextVer = 1
	}
	_, err := s.inner.Conn.ExecContext(ctx,
		"INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,priority,namespace_id) VALUES (?1,?2,?3,?4,?5,?6,?7,?8) ON CONFLICT (agentflow_id,version) DO UPDATE SET definition=?4,comment=?6,priority=?7,updated_at=CURRENT_TIMESTAMP",
		uuid.New().String(), spec.ID, nextVer, b, createdBy, comment, string(spec.Priority), spec.Namespace)
	if err != nil {
		return err
	}
	_, _ = s.inner.Conn.ExecContext(ctx,
		"DELETE FROM orh_agentflow WHERE agentflow_id=?1 AND version NOT IN (SELECT version FROM orh_agentflow WHERE agentflow_id=?1 ORDER BY version DESC LIMIT 10)", spec.ID)
	return nil
}
func (s *FlowSQLiteStore) GetSpec(ctx context.Context, id string) (*entities.FlowInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	row := s.inner.Conn.QueryRowContext(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE agentflow_id=?1 ORDER BY version DESC LIMIT 1", id)
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
