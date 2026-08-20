package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

type KnowledgeSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.KnowledgeEntry]
	conn  *sql.DB
}

func NewKnowledgeSQLiteStore(conn *sql.DB) *KnowledgeSQLiteStore {
	return &KnowledgeSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.KnowledgeEntry]{Conn: conn, Table: "knowledge_entries", IDCol: "id"},
		conn:  conn,
	}
}

func (s *KnowledgeSQLiteStore) Get(ctx context.Context, namespace, id string) (*entities.KnowledgeEntry, error) {
	row := s.conn.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT %s FROM knowledge_entries WHERE namespace_id=? AND id=? AND del_flag=0 LIMIT 1",
		utils.Columns[entities.KnowledgeEntry]()), namespace, id)
	entry := new(entities.KnowledgeEntry)
	if err := utils.ScanStruct(row, entry); err != nil {
		return nil, fmt.Errorf("knowledge entry: %w", err)
	}
	return entry, nil
}

func (s *KnowledgeSQLiteStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.KnowledgeEntry], error) {
	page := normalizePage(filter.Page)
	clauses := []string{"namespace_id=?", "del_flag=0"}
	args := []any{filter.Namespace}
	if filter.Scope != "" {
		clauses = append(clauses, "json_extract(metadata, '$.scope')=?")
		args = append(args, filter.Scope)
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, len(filter.Tags))
		for i, tag := range filter.Tags {
			placeholders[i] = "?"
			args = append(args, tag)
		}
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(COALESCE(tags, '[]')) WHERE value IN ("+strings.Join(placeholders, ",")+"))")
	}
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.conn.QueryRowContext(ctx, "SELECT COUNT(1) FROM knowledge_entries WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(append([]any{}, args...), page.Size, (page.Page-1)*page.Size)
	rows, err := s.conn.QueryContext(ctx, fmt.Sprintf(
		"SELECT %s FROM knowledge_entries WHERE %s ORDER BY created_at DESC LIMIT ? OFFSET ?",
		utils.Columns[entities.KnowledgeEntry](), where), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanSQLiteEntries(rows)
	if err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, page), nil
}

func (s *KnowledgeSQLiteStore) Save(ctx context.Context, entry *entities.KnowledgeEntry) error {
	return s.inner.Save(ctx, entry)
}

func (s *KnowledgeSQLiteStore) Delete(ctx context.Context, namespace, id string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE knowledge_entries SET del_flag=1,status='DELETED',updated_at=CURRENT_TIMESTAMP
		WHERE namespace_id=? AND id=? AND del_flag=0`, namespace, id)
	return err
}

func (s *KnowledgeSQLiteStore) Search(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error) {
	terms := searchTerms(req.Query)
	if len(terms) == 0 {
		return []*entities.KnowledgeEntry{}, nil
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 20
	}
	clauses := []string{"namespace_id=?", "del_flag=0"}
	args := []any{namespace}
	if req.Scope != "" {
		clauses = append(clauses, "json_extract(metadata, '$.scope')=?")
		args = append(args, req.Scope)
	}
	termClauses := make([]string, len(terms))
	for i, term := range terms {
		termClauses[i] = "(LOWER(title) LIKE '%' || ? || '%' OR LOWER(content) LIKE '%' || ? || '%')"
		args = append(args, term, term)
	}
	clauses = append(clauses, "("+strings.Join(termClauses, " OR ")+")")
	if len(req.Tags) > 0 {
		placeholders := make([]string, len(req.Tags))
		for i, tag := range req.Tags {
			placeholders[i] = "?"
			args = append(args, tag)
		}
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(COALESCE(tags, '[]')) WHERE value IN ("+strings.Join(placeholders, ",")+"))")
	}
	args = append(args, topK)
	rows, err := s.conn.QueryContext(ctx, fmt.Sprintf(
		"SELECT %s FROM knowledge_entries WHERE %s ORDER BY created_at DESC LIMIT ?",
		utils.Columns[entities.KnowledgeEntry](), strings.Join(clauses, " AND ")), args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	defer rows.Close()
	return scanSQLiteEntries(rows)
}

func (s *KnowledgeSQLiteStore) ListTags(ctx context.Context, namespace, scope string) ([]string, error) {
	query := `SELECT DISTINCT tag.value FROM knowledge_entries
		JOIN json_each(COALESCE(knowledge_entries.tags, '[]')) AS tag
		WHERE namespace_id=? AND del_flag=0`
	args := []any{namespace}
	if scope != "" {
		query += " AND json_extract(metadata, '$.scope')=?"
		args = append(args, scope)
	}
	query += " ORDER BY tag.value"
	rows, err := s.conn.QueryContext(ctx, query, args...)
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

func scanSQLiteEntries(rows *sql.Rows) ([]*entities.KnowledgeEntry, error) {
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
