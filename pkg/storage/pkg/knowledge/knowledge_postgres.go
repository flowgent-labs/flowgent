package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type KnowledgePostgresStore struct {
	inner *storage.PostgresGenericStore[entities.KnowledgeEntry]
	pool  *pgxpool.Pool
}

func NewKnowledgePostgresStore(pool *pgxpool.Pool) *KnowledgePostgresStore {
	return &KnowledgePostgresStore{
		inner: &storage.PostgresGenericStore[entities.KnowledgeEntry]{Pool: pool, Table: "knowledge_entries", IDCol: "id"},
		pool:  pool,
	}
}

func (s *KnowledgePostgresStore) Get(ctx context.Context, namespace, id string) (*entities.KnowledgeEntry, error) {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		"SELECT %s FROM knowledge_entries WHERE namespace_id=$1 AND id=$2 AND del_flag=false AND (%s) LIMIT 1",
		utils.Columns[entities.KnowledgeEntry](), scopeWhere), args...)
	entry := new(entities.KnowledgeEntry)
	if err := utils.ScanStruct(row, entry); err != nil {
		return nil, fmt.Errorf("knowledge entry: %w", err)
	}
	return entry, nil
}

func (s *KnowledgePostgresStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.KnowledgeEntry], error) {
	page := normalizePage(filter.Page)
	clauses := []string{"namespace_id=$1", "del_flag=false"}
	args := []any{filter.Namespace}
	if filter.Scope != "" {
		args = append(args, filter.Scope)
		clauses = append(clauses, fmt.Sprintf("metadata->>'scope'=$%d", len(args)))
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, len(filter.Tags))
		for i, tag := range filter.Tags {
			args = append(args, tag)
			placeholders[i] = fmt.Sprintf("$%d", len(args))
		}
		clauses = append(clauses, "tags ?| ARRAY["+strings.Join(placeholders, ",")+"]")
	}
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(len(args) + 1)
	clauses = append(clauses, "("+scopeWhere+")")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(1) FROM knowledge_entries WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, page.Size, (page.Page-1)*page.Size)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		"SELECT %s FROM knowledge_entries WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		utils.Columns[entities.KnowledgeEntry](), where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanPostgresEntries(rows)
	if err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, page), nil
}

func (s *KnowledgePostgresStore) Save(ctx context.Context, entry *entities.KnowledgeEntry) error {
	return s.inner.Save(ctx, entry)
}

func (s *KnowledgePostgresStore) Delete(ctx context.Context, namespace, id string) error {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	_, err := s.pool.Exec(ctx, `UPDATE knowledge_entries SET del_flag=true,status='DELETED',updated_at=NOW()
		WHERE namespace_id=$1 AND id=$2 AND del_flag=false AND (`+scopeWhere+`)`, args...)
	return err
}

func (s *KnowledgePostgresStore) Search(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error) {
	terms := searchTerms(req.Query)
	if len(terms) == 0 {
		return []*entities.KnowledgeEntry{}, nil
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 20
	}
	clauses := []string{"namespace_id=$1", "del_flag=false"}
	args := []any{namespace}
	if req.Scope != "" {
		args = append(args, req.Scope)
		clauses = append(clauses, fmt.Sprintf("metadata->>'scope'=$%d", len(args)))
	}
	termClauses := make([]string, len(terms))
	for i, term := range terms {
		args = append(args, term)
		termClauses[i] = fmt.Sprintf("(title ILIKE '%%' || $%d || '%%' OR content ILIKE '%%' || $%d || '%%')", len(args), len(args))
	}
	clauses = append(clauses, "("+strings.Join(termClauses, " OR ")+")")
	if len(req.Tags) > 0 {
		placeholders := make([]string, len(req.Tags))
		for i, tag := range req.Tags {
			args = append(args, tag)
			placeholders[i] = fmt.Sprintf("$%d", len(args))
		}
		clauses = append(clauses, "tags ?| ARRAY["+strings.Join(placeholders, ",")+"]")
	}
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(len(args) + 1)
	clauses = append(clauses, "("+scopeWhere+")")
	args = append(args, scopeArgs...)
	args = append(args, topK)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		"SELECT %s FROM knowledge_entries WHERE %s ORDER BY created_at DESC LIMIT $%d",
		utils.Columns[entities.KnowledgeEntry](), strings.Join(clauses, " AND "), len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	defer rows.Close()
	return scanPostgresEntries(rows)
}

func (s *KnowledgePostgresStore) ListTags(ctx context.Context, namespace, scope string) ([]string, error) {
	query := `SELECT DISTINCT tag.value FROM knowledge_entries
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(tags, '[]'::jsonb)) AS tag(value)
		WHERE namespace_id=$1 AND del_flag=false`
	args := []any{namespace}
	if scope != "" {
		args = append(args, scope)
		query += fmt.Sprintf(" AND metadata->>'scope'=$%d", len(args))
	}
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(len(args) + 1)
	query += " AND (" + scopeWhere + ")"
	args = append(args, scopeArgs...)
	query += " ORDER BY tag.value"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge list tags: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		result = append(result, tag)
	}
	return result, rows.Err()
}

func scanPostgresEntries(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*entities.KnowledgeEntry, error) {
	items := make([]*entities.KnowledgeEntry, 0)
	for rows.Next() {
		entry := new(entities.KnowledgeEntry)
		if err := utils.ScanStruct(rows, entry); err != nil {
			return nil, fmt.Errorf("scan knowledge entry: %w", err)
		}
		items = append(items, entry)
	}
	return items, rows.Err()
}

func normalizePage(page entities.PageRequest) entities.PageRequest {
	if page.Page < 1 {
		page.Page = 1
	}
	if page.Size < 1 {
		page.Size = 20
	}
	return page
}
