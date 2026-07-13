package agent

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentPostgresStore wraps store.PostgresGenericStore[entities.AgentInfo].
type AgentPostgresStore struct {
	inner *store.PostgresGenericStore[entities.AgentInfo]
}

func NewAgentPostgresStore(pool *pgxpool.Pool) *AgentPostgresStore {
	return &AgentPostgresStore{
		inner: &store.PostgresGenericStore[entities.AgentInfo]{
			Pool: pool, Table: "llm_agent", IDCol: "name",
		},
	}
}

func (s *AgentPostgresStore) Get(ctx context.Context, name string) (*entities.AgentInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *AgentPostgresStore) Save(ctx context.Context, e *entities.AgentInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
