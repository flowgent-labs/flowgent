package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
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
	item, err := lookup.GetScoped(ctx, namespace, name)
	if err == nil {
		item.NormalizeAliases()
	}
	return item, err
}
func (s *MCPPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.McpInfo], error) {
	page, err := s.inner.SelectScoped(ctx, namespace, req)
	if err == nil {
		for _, item := range page.Items {
			item.NormalizeAliases()
		}
	}
	return page, err
}
func (s *MCPPostgresStore) Save(ctx context.Context, e *entities.McpInfo) error {
	e.NormalizeAliases()
	if e.RowVersion == 0 {
		e.RowVersion = 1
	}
	if e.Status == "" {
		e.Status = "ACTIVE"
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if _, err := s.inner.Pool.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by) VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255)) ON CONFLICT(id) DO NOTHING`, e.Namespace, e.CreatedBy); err != nil {
		return err
	}
	cols, args := utils.StructFields(e)
	holders := make([]string, len(cols))
	updates := make([]string, 0, len(cols))
	for i, col := range cols {
		holders[i] = fmt.Sprintf("$%d", i+1)
		if col != "id" && col != "created_at" && col != "created_by" && col != "row_version" {
			updates = append(updates, fmt.Sprintf(`"%s"=EXCLUDED."%s"`, col, col))
		}
	}
	query := fmt.Sprintf(`INSERT INTO llm_mcp(%s) VALUES(%s) ON CONFLICT(id) DO UPDATE SET %s WHERE llm_mcp.row_version=EXCLUDED.row_version`, strings.Join(cols, ","), strings.Join(holders, ","), strings.Join(updates, ","))
	result, err := s.inner.Pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("MCP CAS conflict")
	}
	return nil
}
func (s *MCPPostgresStore) Delete(ctx context.Context, namespace, name string) error {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.DeleteScoped(ctx, namespace, name)
}
