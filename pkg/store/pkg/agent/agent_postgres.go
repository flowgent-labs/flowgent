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
			Pool: pool, Table: "llm_agent", IDCol: "id",
		},
	}
}

func (s *AgentPostgresStore) Get(ctx context.Context, namespace, name string) (*entities.AgentInfo, error) {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.GetScoped(ctx, namespace, name)
}
func (s *AgentPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *AgentPostgresStore) Save(ctx context.Context, e *entities.AgentInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentPostgresStore) Delete(ctx context.Context, namespace, name string) error {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.DeleteScoped(ctx, namespace, name)
}
