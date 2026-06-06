package agentdef

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"github.com/flowgent-labs/flowgent/common/src/utils"
)

// AgentDefPostgresStore wraps store.PostgresGenericStore[model.AgentDef].
type AgentDefPostgresStore struct {
	inner *store.PostgresGenericStore[model.AgentDef]
}

func NewAgentDefPostgresStore(pool *pgxpool.Pool) *AgentDefPostgresStore {
	return &AgentDefPostgresStore{
		inner: &store.PostgresGenericStore[model.AgentDef]{
			Pool: pool, Table: "agents", IDCol: "name",
		},
	}
}

func (s *AgentDefPostgresStore) Get(ctx context.Context, name string) (*model.AgentDef, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentDefPostgresStore) Select(ctx context.Context, page, pageSize int) (*utils.Page[model.AgentDef], error) {
	return s.inner.Select(ctx, page, pageSize)
}
func (s *AgentDefPostgresStore) Save(ctx context.Context, e *model.AgentDef) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentDefPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
