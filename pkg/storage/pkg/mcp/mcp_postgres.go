package mcp

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MCPPostgresStore wraps storage.PostgresGenericStore[entities.McpInfo].
type MCPPostgresStore struct {
	inner *storage.PostgresGenericStore[entities.McpInfo]
}

func NewMCPPostgresStore(pool *pgxpool.Pool) *MCPPostgresStore {
	return &MCPPostgresStore{
		inner: &storage.PostgresGenericStore[entities.McpInfo]{Pool: pool, Table: "llm_mcp", IDCol: "id"},
	}
}

func (s *MCPPostgresStore) Get(ctx context.Context, namespace, name string) (*entities.McpInfo, error) {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.GetScoped(ctx, namespace, name)
}
func (s *MCPPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.McpInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *MCPPostgresStore) Save(ctx context.Context, e *entities.McpInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *MCPPostgresStore) Delete(ctx context.Context, namespace, name string) error {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.DeleteScoped(ctx, namespace, name)
}
