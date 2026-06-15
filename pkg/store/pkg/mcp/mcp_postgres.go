package mcp

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MCPPostgresStore wraps store.PostgresGenericStore[entities.McpInfo].
type MCPPostgresStore struct {
	inner *store.PostgresGenericStore[entities.McpInfo]
}

func NewMCPPostgresStore(pool *pgxpool.Pool) *MCPPostgresStore {
	return &MCPPostgresStore{
		inner: &store.PostgresGenericStore[entities.McpInfo]{Pool: pool, Table: "llm_mcp", IDCol: "name"},
	}
}

func (s *MCPPostgresStore) Get(ctx context.Context, name string) (*entities.McpInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *MCPPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.McpInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *MCPPostgresStore) Save(ctx context.Context, e *entities.McpInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *MCPPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
