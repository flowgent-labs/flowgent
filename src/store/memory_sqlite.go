package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/flowgent-labs/flowgent/src/model"
)

// SQLiteMemStore implements Store using SQLite with vector embedding support.
type SQLiteMemStore struct {
	db *sql.DB
}

// NewSQLiteMemStore creates a memory store backed by SQLite.
func NewSQLiteMemStore(db *sql.DB) (*SQLiteMemStore, error) {
	s := &SQLiteMemStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteMemStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agent_memories (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			agentflow_run_id TEXT,
			type TEXT NOT NULL DEFAULT 'episodic',
			content TEXT NOT NULL,
			embedding TEXT,
			tags TEXT DEFAULT '[]',
			metadata TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_mem_agent ON agent_memories(agent_id);
		CREATE INDEX IF NOT EXISTS idx_mem_type ON agent_memories(type);
		CREATE INDEX IF NOT EXISTS idx_mem_af ON agent_memories(agentflow_run_id);

		CREATE TABLE IF NOT EXISTS knowledge_base (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding TEXT,
			tags TEXT DEFAULT '[]',
			source TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_kb_cat ON knowledge_base(category);
	`)
	return err
}

func (s *SQLiteMemStore) SaveMemory(ctx context.Context, m *model.Memory) error {
	now := time.Now()
	if m.ID == "" {
		m.ID = uuid.New().String()
		m.CreatedAt = now
	}
	embeddingJSON := toFloatJSON(m.Embedding)
	tagsJSON := toStringJSON(m.Tags)
	metadataJSON := toJSON(m.Metadata)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO agent_memories (id, agent_id, agentflow_run_id, type, content, embedding, tags, metadata, created_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9)`,
		m.ID, m.AgentID, m.AgentFlowRunID, m.Type, m.Content, embeddingJSON, tagsJSON, metadataJSON, m.CreatedAt)
	return err
}

func (s *SQLiteMemStore) GetMemory(ctx context.Context, id string) (*model.Memory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, agentflow_run_id, type, content, embedding, tags, metadata, created_at FROM agent_memories WHERE id=?1`, id)
	return scanMemory(row)
}

func (s *SQLiteMemStore) ListMemories(ctx context.Context, agentID string, memType model.MemoryType, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, agent_id, agentflow_run_id, type, content, embedding, tags, metadata, created_at FROM agent_memories WHERE agent_id=?1`
	args := []any{agentID}
	if memType != "" {
		q += " AND type=?2"
		args = append(args, string(memType))
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", len(args)+1)
	args = append(args, limit)
	return s.queryMemories(ctx, q, args...)
}

func (s *SQLiteMemStore) DeleteMemory(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM agent_memories WHERE id=?1", id)
	return err
}

func (s *SQLiteMemStore) UpdateMemory(ctx context.Context, m *model.Memory) error {
	embeddingJSON := toFloatJSON(m.Embedding)
	tagsJSON := toStringJSON(m.Tags)
	metadataJSON := toJSON(m.Metadata)
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_memories SET content=?1, embedding=?2, tags=?3, metadata=?4 WHERE id=?5`,
		m.Content, embeddingJSON, tagsJSON, metadataJSON, m.ID)
	return err
}

func (s *SQLiteMemStore) SearchMemories(ctx context.Context, agentID string, embedding []float32, topK int) ([]model.Memory, error) {
	if topK <= 0 {
		topK = 5
	}
	// Load all memories for this agent and compute cosine similarity in Go
	mems, err := s.ListMemories(ctx, agentID, "", 0)
	if err != nil {
		return nil, err
	}
	return topKBySimilarity(mems, embedding, topK), nil
}

func (s *SQLiteMemStore) SaveKnowledge(ctx context.Context, k *model.KnowledgeEntry) error {
	now := time.Now()
	if k.ID == "" {
		k.ID = uuid.New().String()
		k.CreatedAt = now
	}
	k.UpdatedAt = now
	embeddingJSON := toFloatJSON(k.Embedding)
	tagsJSON := toStringJSON(k.Tags)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO knowledge_base (id, category, title, content, embedding, tags, source, created_at, updated_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9)`,
		k.ID, k.Category, k.Title, k.Content, embeddingJSON, tagsJSON, k.Source, k.CreatedAt, k.UpdatedAt)
	return err
}

func (s *SQLiteMemStore) GetKnowledge(ctx context.Context, id string) (*model.KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, category, title, content, embedding, tags, source, created_at, updated_at FROM knowledge_base WHERE id=?1`, id)
	return scanKnowledge(row)
}

func (s *SQLiteMemStore) SearchKnowledge(ctx context.Context, embedding []float32, category string, topK int) ([]model.KnowledgeEntry, error) {
	if topK <= 0 {
		topK = 5
	}
	entries, err := s.ListKnowledge(ctx, category, 0)
	if err != nil {
		return nil, err
	}
	return topKKnowledgeBySimilarity(entries, embedding, topK), nil
}

func (s *SQLiteMemStore) ListKnowledge(ctx context.Context, category string, limit int) ([]model.KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, category, title, content, embedding, tags, source, created_at, updated_at FROM knowledge_base`
	args := []any{}
	if category != "" {
		q += " WHERE category=?1"
		args = append(args, category)
	}
	q += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT %d", len(args)+1)
	args = append(args, limit)
	return s.queryKnowledge(ctx, q, args...)
}

func (s *SQLiteMemStore) DeleteKnowledge(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM knowledge_base WHERE id=?1", id)
	return err
}

func (s *SQLiteMemStore) Close() error {
	return s.db.Close()
}

// ─── Scan helpers ──────────────────────────────

func (s *SQLiteMemStore) queryMemories(ctx context.Context, q string, args ...any) ([]model.Memory, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mems []model.Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		mems = append(mems, *m)
	}
	return mems, rows.Err()
}

func (s *SQLiteMemStore) queryKnowledge(ctx context.Context, q string, args ...any) ([]model.KnowledgeEntry, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []model.KnowledgeEntry
	for rows.Next() {
		k, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *k)
	}
	return entries, rows.Err()
}

// ─── Row scanners ─────────────────────────────

func scanMemory(s scanner) (*model.Memory, error) {
	var m model.Memory
	var emb, tags, meta sql.NullString
	if err := s.Scan(&m.ID, &m.AgentID, &m.AgentFlowRunID, &m.Type, &m.Content, &emb, &tags, &meta, &m.CreatedAt); err != nil {
		return nil, err
	}
	if emb.Valid {
		json.Unmarshal([]byte(emb.String), &m.Embedding)
	}
	if tags.Valid {
		json.Unmarshal([]byte(tags.String), &m.Tags)
	}
	if meta.Valid {
		json.Unmarshal([]byte(meta.String), &m.Metadata)
	}
	return &m, nil
}

func scanKnowledge(s scanner) (*model.KnowledgeEntry, error) {
	var k model.KnowledgeEntry
	var emb, tags sql.NullString
	if err := s.Scan(&k.ID, &k.Category, &k.Title, &k.Content, &emb, &tags, &k.Source, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return nil, err
	}
	if emb.Valid {
		json.Unmarshal([]byte(emb.String), &k.Embedding)
	}
	if tags.Valid {
		json.Unmarshal([]byte(tags.String), &k.Tags)
	}
	return &k, nil
}

// ─── Helpers ──────────────────────────────────

func toFloatJSON(f []float32) []byte {
	if f == nil {
		return nil
	}
	return toJSON(f)
}

func toStringJSON(ss []string) []byte {
	if ss == nil {
		return []byte("[]")
	}
	return toJSON(ss)
}

// ─── Cosine similarity for in-memory topK ─────

type scored[T any] struct {
	item  T
	score float64
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, norma, normb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		norma += float64(a[i]) * float64(a[i])
		normb += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(norma) * math.Sqrt(normb)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func topKBySimilarity(mems []model.Memory, query []float32, k int) []model.Memory {
	var scoredList []scored[model.Memory]
	for _, m := range mems {
		s := cosineSim(query, m.Embedding)
		scoredList = append(scoredList, scored[model.Memory]{m, s})
	}
	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].score > scoredList[j].score })
	if k > len(scoredList) {
		k = len(scoredList)
	}
	result := make([]model.Memory, k)
	for i := 0; i < k; i++ {
		result[i] = scoredList[i].item
	}
	return result
}

func topKKnowledgeBySimilarity(entries []model.KnowledgeEntry, query []float32, k int) []model.KnowledgeEntry {
	var scoredList []scored[model.KnowledgeEntry]
	for _, e := range entries {
		s := cosineSim(query, e.Embedding)
		scoredList = append(scoredList, scored[model.KnowledgeEntry]{e, s})
	}
	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].score > scoredList[j].score })
	if k > len(scoredList) {
		k = len(scoredList)
	}
	result := make([]model.KnowledgeEntry, k)
	for i := 0; i < k; i++ {
		result[i] = scoredList[i].item
	}
	return result
}
// ExecutionPlan stubs (TODO: full implementation)
func (s *SQLiteMemStore) SaveExecutionPlan(ctx context.Context, plan *model.ExecutionPlan) error { return nil }
func (s *SQLiteMemStore) LoadExecutionPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error) { return nil, nil }
func (s *SQLiteMemStore) ListExecutionPlans(ctx context.Context, agentFlowRunID string) ([]*model.ExecutionPlan, error) { return nil, nil }
func (s *SQLiteMemStore) SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error { return nil }
func (s *SQLiteMemStore) LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error) { return nil, nil }
func (s *SQLiteMemStore) ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error { return nil }
func (s *SQLiteMemStore) ReleaseLease(ctx context.Context, planID string) error { return nil }
