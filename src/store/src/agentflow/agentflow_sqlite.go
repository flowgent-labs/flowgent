package agentflow

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"github.com/flowgent-labs/flowgent/common/src/utils"
)

type AgentFlowSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.AgentFlowVersion]
}

func NewAgentFlowSQLiteStore(conn *sql.DB) *AgentFlowSQLiteStore {
	return &AgentFlowSQLiteStore{
		inner: &store.SQLiteGenericStore[model.AgentFlowVersion]{
			Conn: conn, Table: "agentflow_definitions", IDCol: "agentflow_id",
		},
	}
}
func (s *AgentFlowSQLiteStore) Get(ctx context.Context, id string) (*model.AgentFlowVersion, error) { return s.inner.Get(ctx, id) }
func (s *AgentFlowSQLiteStore) Select(ctx context.Context, page, pageSize int) (*utils.Page[model.AgentFlowVersion], error) { return s.inner.Select(ctx, page, pageSize) }
func (s *AgentFlowSQLiteStore) Save(ctx context.Context, e *model.AgentFlowVersion) error { return s.inner.Save(ctx, e) }
func (s *AgentFlowSQLiteStore) Delete(ctx context.Context, id string) error { return s.inner.Delete(ctx, id) }
func (s *AgentFlowSQLiteStore) GetVersion(ctx context.Context, id string, ver int64) (*model.AgentFlowVersion, error) {
	row := s.inner.Conn.QueryRowContext(ctx, "SELECT * FROM agentflow_definitions WHERE agentflow_id=?1 AND version=?2", id, ver)
	var v model.AgentFlowVersion; var b []byte
	if err := row.Scan(&v.AgentFlowID, &v.Version, &b, &v.CreatedBy, &v.Comment, &v.CreatedAt); err != nil { return nil, err }
	v.Definition = b; return &v, nil
}
func (s *AgentFlowSQLiteStore) SaveSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	b, _ := json.Marshal(spec)
	var nextVer int64
	_ = s.inner.Conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM agentflow_definitions WHERE agentflow_id=?1", spec.ID).Scan(&nextVer)
	if nextVer == 0 { nextVer = 1 }
	_, err := s.inner.Conn.ExecContext(ctx, "INSERT OR REPLACE INTO agentflow_definitions (agentflow_id,version,definition,created_by,comment,priority,tenant_id) VALUES (?1,?2,?3,?4,?5,?6,?7)", spec.ID, nextVer, b, createdBy, comment, string(spec.Priority), spec.TenantID)
	return err
}
func (s *AgentFlowSQLiteStore) GetSpec(ctx context.Context, id string) (*model.AgentFlowSpec, error) {
	v, err := s.Get(ctx, id); if err != nil { return nil, err }
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(v.Definition, &spec); err != nil { return nil, err }
	return &spec, nil
}
