package mcp

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// MCPSQLiteStore wraps store.SQLiteGenericStore[entities.McpInfo].
type MCPSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.McpInfo]
}

func NewMCPSQLiteStore(conn *sql.DB) *MCPSQLiteStore {
	return &MCPSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.McpInfo]{Conn: conn, Table: "llm_mcp", IDCol: "name"},
	}
}

func (s *MCPSQLiteStore) Get(ctx context.Context, name string) (*entities.McpInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *MCPSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.McpInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *MCPSQLiteStore) Save(ctx context.Context, e *entities.McpInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *MCPSQLiteStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
