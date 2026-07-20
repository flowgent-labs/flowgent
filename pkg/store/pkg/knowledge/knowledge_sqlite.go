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

// KnowledgeSQLiteStore wraps store.SQLiteGenericStore[entities.KnowledgeEntry]
// and adds custom search and upsert methods.
type KnowledgeSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.KnowledgeEntry]
	conn  *sql.DB
}

// NewKnowledgeSQLiteStore creates a new SQLite-backed knowledge store.
func NewKnowledgeSQLiteStore(conn *sql.DB) *KnowledgeSQLiteStore {
	return &KnowledgeSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.KnowledgeEntry]{
			Conn: conn, Table: "knowledge_entries", IDCol: "id",
		},
		conn: conn,
	}
}

// ── Generic CRUD (delegated to inner store) ─────────────────────────

func (s *KnowledgeSQLiteStore) Get(ctx context.Context, id string) (*entities.KnowledgeEntry, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}
	cols := utils.Columns[entities.KnowledgeEntry]()
	row := s.conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM knowledge_entries WHERE id=?1 AND del_flag=0 LIMIT 1", cols), id)
	var entity entities.KnowledgeEntry
	if err := utils.ScanStruct(row, &entity); err != nil {
		return nil, fmt.Errorf("knowledge_entries: %w", err)
	}
	return &entity, nil
}

func (s *KnowledgeSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.KnowledgeEntry], error) {
	return s.inner.Select(ctx, req)
}

func (s *KnowledgeSQLiteStore) Save(ctx context.Context, e *entities.KnowledgeEntry) error {
	return s.inner.Save(ctx, e)
}

func (s *KnowledgeSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// ── Custom methods ──────────────────────────────────────────────────

// Search performs keyword search on title and content with optional tag filter.
func (s *KnowledgeSQLiteStore) Search(ctx context.Context, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}
	cols := utils.Columns[entities.KnowledgeEntry]()

	topK := req.TopK
	if topK <= 0 {
		topK = 20
	}

	// Build parameterized query
	query := fmt.Sprintf(`SELECT %s FROM knowledge_entries WHERE del_flag = 0 AND (title LIKE '%%' || ?1 || '%%' OR content LIKE '%%' || ?1 || '%%')`, cols)
	args := []any{req.Query}

	if len(req.Tags) > 0 {
		tagConditions := make([]string, len(req.Tags))
		for i, tag := range req.Tags {
			paramIdx := len(args) + 1
			tagConditions[i] = fmt.Sprintf("INSTR(tags, ?%d) > 0", paramIdx)
			args = append(args, fmt.Sprintf(`"%s"`, tag))
		}
		query += " AND (" + strings.Join(tagConditions, " OR ") + ")"
	}

	paramIdx := len(args) + 1
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT ?%d", paramIdx)
	args = append(args, topK)

	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	defer rows.Close()

	var items []*entities.KnowledgeEntry
	for rows.Next() {
		entity := new(entities.KnowledgeEntry)
		if err := utils.ScanStruct(rows, entity); err != nil {
			return nil, fmt.Errorf("knowledge search scan: %w", err)
		}
		items = append(items, entity)
	}
	return items, nil
}

// SearchByTags returns entries matching any of the given tags.
func (s *KnowledgeSQLiteStore) SearchByTags(ctx context.Context, tags []string) ([]*entities.KnowledgeEntry, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}
	cols := utils.Columns[entities.KnowledgeEntry]()

	if len(tags) == 0 {
		return nil, nil
	}

	// Use INSTR to match tag values in the serialized JSON array.
	// Each tag is quoted in the JSON representation: "tagvalue"
	conditions := make([]string, len(tags))
	args := make([]any, len(tags))
	for i, tag := range tags {
		conditions[i] = fmt.Sprintf("INSTR(tags, ?%d) > 0", i+1)
		args[i] = fmt.Sprintf(`"%s"`, tag)
	}

	query := fmt.Sprintf(`SELECT %s FROM knowledge_entries WHERE del_flag = 0 AND (%s) ORDER BY created_at DESC`,
		cols, strings.Join(conditions, " OR "))

	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search by tags: %w", err)
	}
	defer rows.Close()

	var items []*entities.KnowledgeEntry
	for rows.Next() {
		entity := new(entities.KnowledgeEntry)
		if err := utils.ScanStruct(rows, entity); err != nil {
			return nil, fmt.Errorf("knowledge search by tags scan: %w", err)
		}
		items = append(items, entity)
	}
	return items, nil
}

// UpsertBySourceRef creates or updates an entry matched by the source column.
// If an entry with the same source exists it is updated; otherwise a new entry is inserted.
func (s *KnowledgeSQLiteStore) UpsertBySourceRef(ctx context.Context, entity *entities.KnowledgeEntry) error {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return err
	}

	// Check for existing record by source
	var existingID string
	err := s.conn.QueryRowContext(ctx,
		`SELECT id FROM knowledge_entries WHERE source = ?1 AND del_flag = 0 LIMIT 1`,
		entity.Source).Scan(&existingID)

	if err == nil {
		// Use the existing record's ID so the generic store's ON CONFLICT update matches
		entity.ID = existingID
		entity.MarkUpdated("")
	} else if err == sql.ErrNoRows {
		// New entry — set audit fields
		entity.MarkCreated("")
	} else {
		return fmt.Errorf("upsert lookup: %w", err)
	}

	return s.inner.Save(ctx, entity)
}

// ListTags returns all distinct tag values across non-deleted entries.
// Uses json_each to extract individual tag strings from the JSON array.
func (s *KnowledgeSQLiteStore) ListTags(ctx context.Context) ([]string, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}

	// Use json_each to extract individual elements from the tags JSON array.
	// If json_each is not available (older SQLite builds), fall back to
	// iterating all rows and deduplicating in Go.
	rows, err := s.conn.QueryContext(ctx,
		`SELECT DISTINCT e.value FROM knowledge_entries, json_each(knowledge_entries.tags) AS e WHERE knowledge_entries.del_flag = 0 ORDER BY e.value`)
	if err != nil {
		// Fallback: collect all tags and deduplicate in Go
		return s.listTagsFallback(ctx)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("knowledge list tags scan: %w", err)
		}
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// listTagsFallback reads all entries and deduplicates tags in Go.
// Used when SQLite's json_each is not available.
func (s *KnowledgeSQLiteStore) listTagsFallback(ctx context.Context) ([]string, error) {
	cols := utils.Columns[entities.KnowledgeEntry]()
	rows, err := s.conn.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM knowledge_entries WHERE del_flag = 0`, cols))
	if err != nil {
		return nil, fmt.Errorf("knowledge list tags fallback: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		entity := new(entities.KnowledgeEntry)
		if err := utils.ScanStruct(rows, entity); err != nil {
			return nil, fmt.Errorf("knowledge list tags fallback scan: %w", err)
		}
		for _, tag := range entity.Tags {
			seen[tag] = true
		}
	}

	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	return tags, nil
}
