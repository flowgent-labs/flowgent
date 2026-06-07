package agentflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// AgentFlowPostgresStore inherits store.PostgresGenericStore[model.AgentFlowVersion].
type AgentFlowPostgresStore struct {
	inner *store.PostgresGenericStore[model.AgentFlowVersion]
}

func NewAgentFlowPostgresStore(pool *pgxpool.Pool) *AgentFlowPostgresStore {
	return &AgentFlowPostgresStore{
		inner: &store.PostgresGenericStore[model.AgentFlowVersion]{
			Pool: pool, Table: "agentflow_definitions", IDCol: "agentflow_id",
		},
	}
}

func (s *AgentFlowPostgresStore) Get(ctx context.Context, id string) (*model.AgentFlowVersion, error) {
	ver, err := s.inner.Get(ctx, id)
	if err != nil { return nil, err }
	return ver, nil
}
func (s *AgentFlowPostgresStore) Select(ctx context.Context, req model.PageRequest) (*model.Page[model.AgentFlowVersion], error) {
	return s.inner.Select(ctx, req)
}
func (s *AgentFlowPostgresStore) Save(ctx context.Context, e *model.AgentFlowVersion) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentFlowPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// Custom queries
func (s *AgentFlowPostgresStore) GetVersion(ctx context.Context, id string, ver int64) (*model.AgentFlowVersion, error) {
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT * FROM agentflow_definitions WHERE agentflow_id=$1 AND version=$2", id, ver)
	if err != nil { return nil, err }
	defer rows.Close()
	if !rows.Next() { return nil, fmt.Errorf("not found") }
	return scanVersion(rows)
}
func (s *AgentFlowPostgresStore) SaveSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	b, _ := json.Marshal(spec)
	var nextVer int64
	_ = s.inner.Pool.QueryRow(ctx,
		"SELECT COALESCE(MAX(version),0)+1 FROM agentflow_definitions WHERE agentflow_id=$1", spec.ID).Scan(&nextVer)
	if nextVer == 0 { nextVer = 1 }
	_, err := s.inner.Pool.Exec(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id,version,definition,created_by,comment,priority,tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (agentflow_id,version) DO NOTHING`,
		spec.ID, nextVer, b, createdBy, comment, string(spec.Priority), spec.TenantID)
	return err
}
func (s *AgentFlowPostgresStore) GetSpec(ctx context.Context, id string) (*model.AgentFlowSpec, error) {
	ver, err := s.Get(ctx, id)
	if err != nil { return nil, err }
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(ver.Definition, &spec); err != nil { return nil, err }
	return &spec, nil
}

func scanVersion(r pgx.Row) (*model.AgentFlowVersion, error) {
	var v model.AgentFlowVersion
	var b []byte
	err := r.Scan(&v.AgentFlowID, &v.Version, &b, &v.CreatedBy, &v.Comment, &v.CreatedAt)
	v.Definition = b
	return &v, err
}
