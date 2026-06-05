package agentdef

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// AgentPostgresStore wraps store.PostgresGenericStore[model.AgentDef].
type AgentPostgresStore struct {
	inner *store.PostgresGenericStore[model.AgentDef]
}

func NewAgentPostgresStore(pool *pgxpool.Pool) *AgentPostgresStore {
	return &AgentPostgresStore{
		inner: &store.PostgresGenericStore[model.AgentDef]{
			Pool: pool, Table: "agents", IDCol: "name",
		},
	}
}

func (s *AgentPostgresStore) Get(ctx context.Context, name string) (*model.AgentDef, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentPostgresStore) Select(ctx context.Context, offset, limit int) ([]*model.AgentDef, error) {
	return s.inner.Select(ctx, offset, limit)
}
func (s *AgentPostgresStore) Save(ctx context.Context, e *model.AgentDef) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
