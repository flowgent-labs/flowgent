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

func (s *LlmProviderSQLiteStore) Get(ctx context.Context, id string) (*entities.LlmProviderInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *LlmProviderSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.LlmProviderInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *LlmProviderSQLiteStore) Save(ctx context.Context, e *entities.LlmProviderInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *LlmProviderSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
