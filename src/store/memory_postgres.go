package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/flowgent-labs/flowgent/src/model"
)

// PgMemStore implements Store using PostgreSQL with vector embedding support.
type PgMemStore struct {
	db *sql.DB
}

// NewPgMemStore creates a memory store backed by a PostgreSQL database.
func NewPgMemStore(db *sql.DB) (*PgMemStore, error) {
	s := &PgMemStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PgMemStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agent_memories (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			agentflow_run_id TEXT,
			type TEXT NOT NULL DEFAULT 'episodic',
			content TEXT NOT NULL,
			embedding JSONB,
			tags TEXT[] DEFAULT '{}',
			metadata JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_mem_agent_pg ON agent_memories(agent_id);
		CREATE INDEX IF NOT EXISTS idx_mem_type_pg ON agent_memories(type);
		CREATE INDEX IF NOT EXISTS idx_mem_af_pg ON agent_memories(agentflow_run_id);

		CREATE TABLE IF NOT EXISTS knowledge_base (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding JSONB,
			tags TEXT[] DEFAULT '{}',
			source TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_kb_cat_pg ON knowledge_base(category);
	`)
	// Note: Full pgvector extension support requires CREATE EXTENSION vector and
	// embedding vector(768) column type. For now embeddings are stored as JSONB arrays
	// and similarity is computed in Go. To enable native pgvector:
	//   CREATE EXTENSION IF NOT EXISTS vector;
	//   ALTER TABLE agent_memories ADD COLUMN embedding_vec vector(768);
	//   UPDATE agent_memories SET embedding_vec = embedding::vector FROM ...;
	return err
}

func (s *PgMemStore) SaveMemory(ctx context.Context, m *model.Memory) error {
	now := time.Now()
	if m.ID == "" {
		m.ID = uuid.New().String()
		m.CreatedAt = now
	}
	metadataJSON := toJSON(m.Metadata)

	// PG text array format: {tag1,tag2}
	tagsArr := toPGArray(m.Tags)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_memories (id, agent_id, agentflow_run_id, type, content, embedding, tags, metadata, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::text[],$8::jsonb,$9)
		 ON CONFLICT (id) DO UPDATE SET content=EXCLUDED.content, embedding=EXCLUDED.embedding,
		 tags=EXCLUDED.tags, metadata=EXCLUDED.metadata`,
		m.ID, m.AgentID, m.AgentFlowRunID, m.Type, m.Content, toJSON(m.Embedding), tagsArr, metadataJSON, m.CreatedAt)
	return err
}

func (s *PgMemStore) GetMemory(ctx context.Context, id string) (*model.Memory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, agentflow_run_id, type, content, embedding::text, tags::text, metadata::text, created_at
		 FROM agent_memories WHERE id=$1`, id)
	return scanMemoryPG(row)
}

func (s *PgMemStore) ListMemories(ctx context.Context, agentID string, memType model.MemoryType, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, agent_id, agentflow_run_id, type, content, embedding::text, tags::text, metadata::text, created_at
		 FROM agent_memories WHERE agent_id=$1`
	args := []any{agentID}
	argIdx := 2
	if memType != "" {
		q += fmt.Sprintf(" AND type=$%d", argIdx)
		args = append(args, string(memType))
		argIdx++
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)
	return s.queryPgMemories(ctx, q, args...)
}

func (s *PgMemStore) DeleteMemory(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM agent_memories WHERE id=$1", id)
	return err
}

func (s *PgMemStore) UpdateMemory(ctx context.Context, m *model.Memory) error {
	tagsArr := toPGArray(m.Tags)
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_memories SET content=$1, embedding=$2::jsonb, tags=$3::text[], metadata=$4::jsonb WHERE id=$5`,
		m.Content, toJSON(m.Embedding), tagsArr, toJSON(m.Metadata), m.ID)
	return err
}

func (s *PgMemStore) SearchMemories(ctx context.Context, agentID string, embedding []float32, topK int) ([]model.Memory, error) {
	// Load all memories and compute cosine similarity in Go
	// (for native pgvector, use: ORDER BY embedding <=> $query LIMIT $k)
	mems, err := s.ListMemories(ctx, agentID, "", 0)
	if err != nil {
		return nil, err
	}
	return topKBySimilarity(mems, embedding, topK), nil
}

func (s *PgMemStore) SaveKnowledge(ctx context.Context, k *model.KnowledgeEntry) error {
	now := time.Now()
	if k.ID == "" {
		k.ID = uuid.New().String()
		k.CreatedAt = now
	}
	k.UpdatedAt = now
	tagsArr := toPGArray(k.Tags)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_base (id, category, title, content, embedding, tags, source, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5::jsonb,$6::text[],$7,$8,$9)
		 ON CONFLICT (id) DO UPDATE SET category=EXCLUDED.category, title=EXCLUDED.title,
		 content=EXCLUDED.content, embedding=EXCLUDED.embedding, tags=EXCLUDED.tags,
		 source=EXCLUDED.source, updated_at=EXCLUDED.updated_at`,
		k.ID, k.Category, k.Title, k.Content, toJSON(k.Embedding), tagsArr, k.Source, k.CreatedAt, k.UpdatedAt)
	return err
}

func (s *PgMemStore) GetKnowledge(ctx context.Context, id string) (*model.KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, category, title, content, embedding::text, tags::text, source, created_at, updated_at
		 FROM knowledge_base WHERE id=$1`, id)
	return scanKnowledgePG(row)
}

func (s *PgMemStore) SearchKnowledge(ctx context.Context, embedding []float32, category string, topK int) ([]model.KnowledgeEntry, error) {
	entries, err := s.ListKnowledge(ctx, category, 0)
	if err != nil {
		return nil, err
	}
	return topKKnowledgeBySimilarity(entries, embedding, topK), nil
}

func (s *PgMemStore) ListKnowledge(ctx context.Context, category string, limit int) ([]model.KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, category, title, content, embedding::text, tags::text, source, created_at, updated_at
		 FROM knowledge_base`
	args := []any{}
	argIdx := 1
	if category != "" {
		q += fmt.Sprintf(" WHERE category=$%d", argIdx)
		args = append(args, category)
		argIdx++
	}
	q += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)
	return s.queryPgKnowledge(ctx, q, args...)
}

func (s *PgMemStore) DeleteKnowledge(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM knowledge_base WHERE id=$1", id)
	return err
}

func (s *PgMemStore) Close() error {
	return s.db.Close()
}

// ─── PG query helpers ─────────────────────────

func (s *PgMemStore) queryPgMemories(ctx context.Context, q string, args ...any) ([]model.Memory, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mems []model.Memory
	for rows.Next() {
		m, err := scanMemoryPG(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, *m)
	}
	return mems, rows.Err()
}

func (s *PgMemStore) queryPgKnowledge(ctx context.Context, q string, args ...any) ([]model.KnowledgeEntry, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []model.KnowledgeEntry
	for rows.Next() {
		k, err := scanKnowledgePG(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *k)
	}
	return entries, rows.Err()
}

// ─── PG row scanners ──────────────────────────

func scanMemoryPG(s scanner) (*model.Memory, error) {
	var m model.Memory
	var embStr, tagsStr, metaStr sql.NullString
	if err := s.Scan(&m.ID, &m.AgentID, &m.AgentFlowRunID, &m.Type, &m.Content, &embStr, &tagsStr, &metaStr, &m.CreatedAt); err != nil {
		return nil, err
	}
	parseEmbedding(embStr, &m.Embedding)
	parseTags(tagsStr, &m.Tags)
	parseMetadata(metaStr, &m.Metadata)
	return &m, nil
}

func scanKnowledgePG(s scanner) (*model.KnowledgeEntry, error) {
	var k model.KnowledgeEntry
	var embStr, tagsStr sql.NullString
	if err := s.Scan(&k.ID, &k.Category, &k.Title, &k.Content, &embStr, &tagsStr, &k.Source, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return nil, err
	}
	parseEmbedding(embStr, &k.Embedding)
	parseTagsArray(tagsStr, &k.Tags)
	return &k, nil
}

func parseEmbedding(s sql.NullString, v *[]float32) {
	if !s.Valid {
		return
	}
	json.Unmarshal([]byte(s.String), v)
}

func parseTags(s sql.NullString, v *[]string) {
	if s.Valid && s.String != "" {
		json.Unmarshal([]byte(s.String), v)
	}
}

func parseTagsArray(s sql.NullString, v *[]string) {
	if !s.Valid || s.String == "" || s.String == "{}" {
		return
	}
	// Parse PG text array: {foo,bar} → ["foo","bar"]
	raw := s.String
	raw = raw[1 : len(raw)-1] // strip {}
	parts := splitPGArray(raw)
	*v = parts
}

func splitPGArray(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	var current string
	inQuote := false
	for _, c := range s {
		switch c {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				result = append(result, current)
				current = ""
			} else {
				current += string(c)
			}
		default:
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func parseMetadata(s sql.NullString, v *map[string]any) {
	if !s.Valid || s.String == "" {
		return
	}
	json.Unmarshal([]byte(s.String), v)
}

func toPGArray(ss []string) string {
	if len(ss) == 0 {
		return "{}"
	}
	result := "{"
	for i, s := range ss {
		if i > 0 {
			result += ","
		}
		result += `"` + s + `"`
	}
	result += "}"
	return result
}
