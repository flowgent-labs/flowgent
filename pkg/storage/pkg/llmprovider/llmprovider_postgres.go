package llmprovider

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LlmProviderPostgresStore wraps storage.PostgresGenericStore[entities.LlmProviderInfo].
type LlmProviderPostgresStore struct {
	inner *storage.PostgresGenericStore[entities.LlmProviderInfo]
}

func NewLlmProviderPostgresStore(pool *pgxpool.Pool) *LlmProviderPostgresStore {
	return &LlmProviderPostgresStore{
		inner: &storage.PostgresGenericStore[entities.LlmProviderInfo]{
			Pool: pool, Table: "llm_providers", IDCol: "id",
		},
	}
}

func (s *LlmProviderPostgresStore) Get(ctx context.Context, namespace, id string) (*entities.LlmProviderInfo, error) {
	return s.inner.GetScoped(ctx, namespace, id)
}
func (s *LlmProviderPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.LlmProviderInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *LlmProviderPostgresStore) Save(ctx context.Context, e *entities.LlmProviderInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *LlmProviderPostgresStore) Delete(ctx context.Context, namespace, id string) error {
	return s.inner.DeleteScoped(ctx, namespace, id)
}
