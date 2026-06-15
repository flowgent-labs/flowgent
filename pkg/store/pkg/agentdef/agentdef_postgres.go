package agentdef

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentDefPostgresStore wraps store.PostgresGenericStore[entities.AgentInfo].
type AgentDefPostgresStore struct {
	inner *store.PostgresGenericStore[entities.AgentInfo]
}

func NewAgentDefPostgresStore(pool *pgxpool.Pool) *AgentDefPostgresStore {
	return &AgentDefPostgresStore{
		inner: &store.PostgresGenericStore[entities.AgentInfo]{
			Pool: pool, Table: "llm_agent", IDCol: "name",
		},
	}
}

func (s *AgentDefPostgresStore) Get(ctx context.Context, name string) (*entities.AgentInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentDefPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *AgentDefPostgresStore) Save(ctx context.Context, e *entities.AgentInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentDefPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
