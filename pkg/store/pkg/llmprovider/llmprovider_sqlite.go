package llmprovider

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// LlmProviderSQLiteStore wraps store.SQLiteGenericStore[entities.LlmProviderInfo].
type LlmProviderSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.LlmProviderInfo]
}

func NewLlmProviderSQLiteStore(conn *sql.DB) *LlmProviderSQLiteStore {
	return &LlmProviderSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.LlmProviderInfo]{
			Conn: conn, Table: "llm_providers", IDCol: "id",
		},
	}
}

func (s *LlmProviderSQLiteStore) Get(ctx context.Context, namespace, id string) (*entities.LlmProviderInfo, error) {
	return s.inner.GetScoped(ctx, namespace, id)
}
func (s *LlmProviderSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.LlmProviderInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *LlmProviderSQLiteStore) Save(ctx context.Context, e *entities.LlmProviderInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *LlmProviderSQLiteStore) Delete(ctx context.Context, namespace, id string) error {
	return s.inner.DeleteScoped(ctx, namespace, id)
}
