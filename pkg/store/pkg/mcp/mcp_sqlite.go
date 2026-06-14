package mcp

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// MCPSQLiteStore wraps store.SQLiteGenericStore[model.MCPDef].
type MCPSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.MCPDef]
}

func NewMCPSQLiteStore(conn *sql.DB) *MCPSQLiteStore {
	return &MCPSQLiteStore{
		inner: &store.SQLiteGenericStore[model.MCPDef]{Conn: conn, Table: "mcps", IDCol: "name"},
	}
}

func (s *MCPSQLiteStore) Get(ctx context.Context, name string) (*model.MCPDef, error) {
	return s.inner.Get(ctx, name)
}
func (s *MCPSQLiteStore) Select(ctx context.Context, req model.PageRequest) (*model.Page[model.MCPDef], error) {
	return s.inner.Select(ctx, req)
}
func (s *MCPSQLiteStore) Save(ctx context.Context, e *model.MCPDef) error {
	return s.inner.Save(ctx, e)
}
func (s *MCPSQLiteStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
