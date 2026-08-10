package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KnowledgePostgresStore wraps store.PostgresGenericStore[entities.KnowledgeEntry]
// and adds custom search and upsert methods.
type KnowledgePostgresStore struct {
	inner *store.PostgresGenericStore[entities.KnowledgeEntry]
	pool  *pgxpool.Pool
}

// NewKnowledgePostgresStore creates a new PG-backed knowledge store.
func NewKnowledgePostgresStore(pool *pgxpool.Pool) *KnowledgePostgresStore {
	return &KnowledgePostgresStore{
		inner: &store.PostgresGenericStore[entities.KnowledgeEntry]{
			Pool: pool, Table: "knowledge_entries", IDCol: "id",
		},
		pool: pool,
	}
}

// ── Generic CRUD (delegated to inner store) ─────────────────────────

func (s *KnowledgePostgresStore) Get(ctx context.Context, id string) (*entities.KnowledgeEntry, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}
	cols := utils.Columns[entities.KnowledgeEntry]()
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM knowledge_entries WHERE id=$1 AND del_flag=false LIMIT 1", cols), id)
	var entity entities.KnowledgeEntry
	if err := utils.ScanStruct(row, &entity); err != nil {
		return nil, fmt.Errorf("knowledge_entries: %w", err)
	}
	return &entity, nil
}

func (s *KnowledgePostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.KnowledgeEntry], error) {
	return s.inner.Select(ctx, req)
}

func (s *KnowledgePostgresStore) Save(ctx context.Context, e *entities.KnowledgeEntry) error {
	return s.inner.Save(ctx, e)
}

func (s *KnowledgePostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// ── Custom methods ──────────────────────────────────────────────────

// Search performs keyword search on title and content with optional tag filter.
func (s *KnowledgePostgresStore) Search(ctx context.Context, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}
	cols := utils.Columns[entities.KnowledgeEntry]()

	topK := req.TopK
	if topK <= 0 {
		topK = 20
	}
	terms := searchTerms(req.Query)
	if len(terms) == 0 {
		return []*entities.KnowledgeEntry{}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`SELECT %s FROM knowledge_entries WHERE del_flag = false AND (`, cols))

	args := make([]any, 0, len(terms)+len(req.Tags)+1)
	termClauses := make([]string, len(terms))
	argIdx := 1
	for i, term := range terms {
		termClauses[i] = fmt.Sprintf(`(title ILIKE '%%' || $%d || '%%' OR content::text ILIKE '%%' || $%d || '%%')`, argIdx, argIdx)
		args = append(args, term)
		argIdx++
	}
	sb.WriteString(strings.Join(termClauses, " OR "))
	sb.WriteString(")")

	if len(req.Tags) > 0 {
		placeholders := make([]string, len(req.Tags))
		for i, tag := range req.Tags {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, tag)
			argIdx++
		}
		sb.WriteString(fmt.Sprintf(` AND tags ?| ARRAY[%s]`, strings.Join(placeholders, ",")))
	}

	sb.WriteString(fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, argIdx))
	args = append(args, topK)

	rows, err := s.pool.Query(ctx, sb.String(), args...)
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
func (s *KnowledgePostgresStore) SearchByTags(ctx context.Context, tags []string) ([]*entities.KnowledgeEntry, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}
	cols := utils.Columns[entities.KnowledgeEntry]()

	if len(tags) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(tags))
	args := make([]any, len(tags))
	for i, tag := range tags {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = tag
	}

	query := fmt.Sprintf(`SELECT %s FROM knowledge_entries WHERE del_flag = false AND tags ?| ARRAY[%s] ORDER BY created_at DESC`,
		cols, strings.Join(placeholders, ","))

	rows, err := s.pool.Query(ctx, query, args...)
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
func (s *KnowledgePostgresStore) UpsertBySourceRef(ctx context.Context, entity *entities.KnowledgeEntry) error {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return err
	}

	// Check for existing record by source
	var existingID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM knowledge_entries WHERE source = $1 AND del_flag = false LIMIT 1`,
		entity.Source).Scan(&existingID)

	if err == nil {
		// Preserve original audit fields from the existing record
		var existingCreatedAt, existingUpdatedAt any
		var existingCreatedBy, existingUpdatedBy string
		err = s.pool.QueryRow(ctx,
			`SELECT created_at, created_by, updated_at, updated_by FROM knowledge_entries WHERE id = $1`,
			existingID).Scan(&existingCreatedAt, &existingCreatedBy, &existingUpdatedAt, &existingUpdatedBy)
		if err == nil {
			entity.ID = existingID
			// Preserve original creation timestamp
			entity.MarkUpdated("")
		} else {
			entity.ID = existingID
		}
	} else if err == pgx.ErrNoRows {
		// New entry — set audit fields
		entity.MarkCreated("")
	} else {
		return fmt.Errorf("upsert lookup: %w", err)
	}

	return s.inner.Save(ctx, entity)
}

// ListTags returns all distinct tag values across non-deleted entries.
// Uses jsonb_array_elements_text to extract individual tag strings from the JSONB array.
func (s *KnowledgePostgresStore) ListTags(ctx context.Context) ([]string, error) {
	if err := utils.ValidateIdent("knowledge_entries"); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT jsonb_array_elements_text(tags) AS tag FROM knowledge_entries WHERE del_flag = false ORDER BY tag`)
	if err != nil {
		return nil, fmt.Errorf("knowledge list tags: %w", err)
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
