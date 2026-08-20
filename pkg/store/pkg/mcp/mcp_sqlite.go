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
		inner: &store.SQLiteGenericStore[entities.McpInfo]{Conn: conn, Table: "llm_mcp", IDCol: "id"},
	}
}

func (s *MCPSQLiteStore) Get(ctx context.Context, namespace, name string) (*entities.McpInfo, error) {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.GetScoped(ctx, namespace, name)
}
func (s *MCPSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.McpInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *MCPSQLiteStore) Save(ctx context.Context, e *entities.McpInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *MCPSQLiteStore) Delete(ctx context.Context, namespace, name string) error {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.DeleteScoped(ctx, namespace, name)
}
