package llmprovider

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// LlmProviderSQLiteStore wraps store.SQLiteGenericStore[model.LlmProvider].
type LlmProviderSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.LlmProvider]
}

func NewLlmProviderSQLiteStore(conn *sql.DB) *LlmProviderSQLiteStore {
	return &LlmProviderSQLiteStore{
		inner: &store.SQLiteGenericStore[model.LlmProvider]{
			Conn: conn, Table: "llm_providers", IDCol: "id",
		},
	}
}

func (s *LlmProviderSQLiteStore) Get(ctx context.Context, id string) (*model.LlmProvider, error) {
	return s.inner.Get(ctx, id)
}
func (s *LlmProviderSQLiteStore) Select(ctx context.Context, offset, limit int) ([]*model.LlmProvider, error) {
	return s.inner.Select(ctx, offset, limit)
}
func (s *LlmProviderSQLiteStore) Save(ctx context.Context, e *model.LlmProvider) error {
	return s.inner.Save(ctx, e)
}
func (s *LlmProviderSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
