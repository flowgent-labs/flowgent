package llmprovider

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// LlmProviderPostgresStore wraps store.PostgresGenericStore[model.LlmProvider].
type LlmProviderPostgresStore struct {
	inner *store.PostgresGenericStore[model.LlmProvider]
}

func NewLlmProviderPostgresStore(pool *pgxpool.Pool) *LlmProviderPostgresStore {
	return &LlmProviderPostgresStore{
		inner: &store.PostgresGenericStore[model.LlmProvider]{
			Pool: pool, Table: "llm_providers", IDCol: "id",
		},
	}
}

func (s *LlmProviderPostgresStore) Get(ctx context.Context, id string) (*model.LlmProvider, error) {
	return s.inner.Get(ctx, id)
}
func (s *LlmProviderPostgresStore) Select(ctx context.Context, page, pageSize int) (*model.Page[model.LlmProvider], error) {
	return s.inner.Select(ctx, page, pageSize)
}
func (s *LlmProviderPostgresStore) Save(ctx context.Context, e *model.LlmProvider) error {
	return s.inner.Save(ctx, e)
}
func (s *LlmProviderPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
