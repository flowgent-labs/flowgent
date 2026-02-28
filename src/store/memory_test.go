package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/flowgent-labs/flowgent/src/model"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

func TestSQLiteMemStore_SaveGet(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, err := NewSQLiteMemStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteMemStore: %v", err)
	}

	ctx := context.Background()
	m := &model.Memory{
		AgentID:        "agent-1",
		AgentFlowRunID: "run-1",
		Type:           model.MemoryEpisodic,
		Content:        "test memory content",
		Embedding:      []float32{0.1, 0.2, 0.3},
		Tags:           []string{"test", "memory"},
		Metadata:       map[string]any{"source": "ut"},
	}
	if err := store.SaveMemory(ctx, m); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if m.ID == "" {
		t.Fatal("ID should be set")
	}

	got, err := store.GetMemory(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Content != "test memory content" {
		t.Errorf("expected content, got %s", got.Content)
	}
	if len(got.Embedding) != 3 {
		t.Errorf("expected 3 embedding dims, got %d", len(got.Embedding))
	}
	if len(got.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(got.Tags))
	}
}

func TestSQLiteMemStore_ListDelete(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, _ := NewSQLiteMemStore(db)
	ctx := context.Background()

	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		m := &model.Memory{AgentID: "a2", Type: model.MemoryEpisodic, Content: "c"}
		store.SaveMemory(ctx, m)
		ids[i] = m.ID
	}

	list, err := store.ListMemories(ctx, "a2", "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2, got %d", len(list))
	}

	store.DeleteMemory(ctx, ids[0])
	list2, _ := store.ListMemories(ctx, "a2", "", 10)
	if len(list2) != 2 {
		t.Errorf("expected 2 after delete, got %d", len(list2))
	}
}

func TestSQLiteMemStore_Search(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, _ := NewSQLiteMemStore(db)
	ctx := context.Background()

	store.SaveMemory(ctx, &model.Memory{AgentID: "a1", Type: model.MemoryEpisodic, Content: "c1", Embedding: []float32{1.0, 0.0, 0.0}})
	store.SaveMemory(ctx, &model.Memory{AgentID: "a1", Type: model.MemoryEpisodic, Content: "c2", Embedding: []float32{0.0, 1.0, 0.0}})
	store.SaveMemory(ctx, &model.Memory{AgentID: "a1", Type: model.MemoryEpisodic, Content: "c3", Embedding: []float32{1.0, 0.0, 0.0}})

	results, err := store.SearchMemories(ctx, "a1", []float32{1.0, 0.0, 0.0}, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 topK, got %d", len(results))
	}
	// First result should be most similar (c1 or c3, with embedding [1,0,0])
	if results[0].Content != "c1" && results[0].Content != "c3" {
		t.Errorf("expected c1 or c3 as top result, got %s", results[0].Content)
	}
}

func TestSQLiteMemStore_Knowledge(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, _ := NewSQLiteMemStore(db)
	ctx := context.Background()

	k := &model.KnowledgeEntry{
		Category:  "patterns",
		Title:     "SQL Injection Fix",
		Content:   "Use parameterized queries",
		Embedding: []float32{0.5, 0.5},
		Tags:      []string{"security", "java"},
		Source:    "confluence",
	}
	if err := store.SaveKnowledge(ctx, k); err != nil {
		t.Fatalf("SaveKnowledge: %v", err)
	}

	got, err := store.GetKnowledge(ctx, k.ID)
	if err != nil {
		t.Fatalf("GetKnowledge: %v", err)
	}
	if got.Title != "SQL Injection Fix" {
		t.Errorf("expected title, got %s", got.Title)
	}

	list, _ := store.ListKnowledge(ctx, "patterns", 10)
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	results, _ := store.SearchKnowledge(ctx, []float32{0.5, 0.5}, "", 1)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	store.DeleteKnowledge(ctx, k.ID)
	list2, _ := store.ListKnowledge(ctx, "patterns", 10)
	if len(list2) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(list2))
	}
}

func TestHelpers(t *testing.T) {
	m := &model.Memory{AgentID: "agent-1", Type: model.MemoryEpisodic, Content: "hello"}
	if m.AgentID != "agent-1" || m.Type != model.MemoryEpisodic {
		t.Error("memory init failed")
	}

	k := &model.KnowledgeEntry{Category: "cat", Title: "title", Content: "body", Tags: []string{"t1"}}
	if k.Category != "cat" || len(k.Tags) != 1 {
		t.Error("kb init failed")
	}
}
