package llmprovider

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

// LlmProviderSQLiteStore wraps storage.SQLiteGenericStore[entities.LlmProviderInfo].
type LlmProviderSQLiteStore struct {
	inner *storage.SQLiteGenericStore[entities.LlmProviderInfo]
}

func NewLlmProviderSQLiteStore(conn *sql.DB) *LlmProviderSQLiteStore {
	return &LlmProviderSQLiteStore{
		inner: &storage.SQLiteGenericStore[entities.LlmProviderInfo]{
			Conn: conn, Table: "llm_provider", IDCol: "id",
		},
	}
}

func (s *LlmProviderSQLiteStore) Get(ctx context.Context, namespace, id string) (*entities.LlmProviderInfo, error) {
	item, err := s.inner.GetScoped(ctx, namespace, id)
	if err == nil {
		item.NormalizeAliases()
		item.ApiKey = item.CredentialRef
	}
	return item, err
}
func (s *LlmProviderSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.LlmProviderInfo], error) {
	page, err := s.inner.SelectScoped(ctx, namespace, req)
	if err == nil {
		for _, item := range page.Items {
			item.NormalizeAliases()
			item.ApiKey = item.CredentialRef
		}
	}
	return page, err
}
func (s *LlmProviderSQLiteStore) Save(ctx context.Context, e *entities.LlmProviderInfo) error {
	e.NormalizeAliases()
	if e.CredentialRef == "" {
		e.CredentialRef = e.ApiKey
	}
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
	if _, err := s.inner.Conn.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace(id,name,description,created_by,updated_by) VALUES(?,?,?, ?,?)`, e.Namespace, e.Namespace, "Flowgent namespace", e.CreatedBy, e.CreatedBy); err != nil {
		return err
	}
	cols, args := utils.StructFields(e)
	holders := make([]string, len(cols))
	updates := make([]string, 0, len(cols))
	for i, col := range cols {
		holders[i] = "?"
		if col != "id" && col != "created_at" && col != "created_by" && col != "row_version" {
			updates = append(updates, fmt.Sprintf(`"%s"=excluded."%s"`, col, col))
		}
	}
	query := fmt.Sprintf(`INSERT INTO llm_provider(%s) VALUES(%s) ON CONFLICT(id) DO UPDATE SET %s WHERE llm_provider.row_version=excluded.row_version`, strings.Join(cols, ","), strings.Join(holders, ","), strings.Join(updates, ","))
	result, err := s.inner.Conn.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("LLM provider CAS conflict")
	}
	return nil
}
func (s *LlmProviderSQLiteStore) Delete(ctx context.Context, namespace, id string) error {
	return s.inner.DeleteScoped(ctx, namespace, id)
}
