package mcp

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MCPPostgresStore wraps store.PostgresGenericStore[model.MCPDef].
type MCPPostgresStore struct {
	inner *store.PostgresGenericStore[model.MCPDef]
}

func NewMCPPostgresStore(pool *pgxpool.Pool) *MCPPostgresStore {
	return &MCPPostgresStore{
		inner: &store.PostgresGenericStore[model.MCPDef]{Pool: pool, Table: "mcps", IDCol: "name"},
	}
}

func (s *MCPPostgresStore) Get(ctx context.Context, name string) (*model.MCPDef, error) {
	return s.inner.Get(ctx, name)
}
func (s *MCPPostgresStore) Select(ctx context.Context, req model.PageRequest) (*model.Page[model.MCPDef], error) {
	return s.inner.Select(ctx, req)
}
func (s *MCPPostgresStore) Save(ctx context.Context, e *model.MCPDef) error {
	return s.inner.Save(ctx, e)
}
func (s *MCPPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
