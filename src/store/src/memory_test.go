package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/flowgent-labs/flowgent/model/src"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

func TestNodeMemory_UpsertGet(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, err := NewSQLiteNodeMemoryStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteNodeMemoryStore: %v", err)
	}

	ctx := context.Background()
	m := &model.NodeMemory{
		FlowID:    "flow-1",
		NodeID:    "detect",
		Content:   "attempt=0 prompt=... response=...",
		Embedding: []float32{0.1, 0.2, 0.3},
		Metadata:  map[string]any{"retry_count": 0},
	}
	if err := store.UpsertMemory(ctx, m); err != nil {
		t.Fatalf("UpsertMemory: %v", err)
	}

	got, err := store.GetMemory(ctx, "flow-1", "detect")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got == nil {
		t.Fatal("expected memory, got nil")
	}
	if got.Content != m.Content {
		t.Errorf("expected %q, got %q", m.Content, got.Content)
	}

	// Upsert should accumulate, not replace
	m2 := &model.NodeMemory{
		FlowID:  "flow-1",
		NodeID:  "detect",
		Content: "attempt=1 prompt=... response=...",
	}
	_ = store.UpsertMemory(ctx, m2)
	got2, _ := store.GetMemory(ctx, "flow-1", "detect")
	if got2.Content == "attempt=1 prompt=... response=..." {
		t.Error("upsert should accumulate content, caller handles that")
	}
}

func TestNodeMemory_FlowScoped(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, _ := NewSQLiteNodeMemoryStore(db)
	ctx := context.Background()

	// Same flow, different nodes
	store.UpsertMemory(ctx, &model.NodeMemory{FlowID: "flow-1", NodeID: "detect", Content: "d"})
	store.UpsertMemory(ctx, &model.NodeMemory{FlowID: "flow-1", NodeID: "fix", Content: "f"})
	store.UpsertMemory(ctx, &model.NodeMemory{FlowID: "flow-2", NodeID: "detect", Content: "d2"})

	list, _ := store.ListFlowMemories(ctx, "flow-1")
	if len(list) != 2 {
		t.Errorf("flow-1: expected 2, got %d", len(list))
	}

	list2, _ := store.ListFlowMemories(ctx, "flow-2")
	if len(list2) != 1 {
		t.Errorf("flow-2: expected 1, got %d", len(list2))
	}
}

func TestNodeMemory_SharedFlowLevel(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, _ := NewSQLiteNodeMemoryStore(db)
	ctx := context.Background()

	// nodeID="" = flow-level shared memory
	store.UpsertMemory(ctx, &model.NodeMemory{FlowID: "flow-1", NodeID: "", Content: "shared"})
	store.UpsertMemory(ctx, &model.NodeMemory{FlowID: "flow-1", NodeID: "detect", Content: "node"})

	shared, _ := store.GetMemory(ctx, "flow-1", "")
	if shared == nil || shared.Content != "shared" {
		t.Error("flow-level shared memory not found")
	}
	node, _ := store.GetMemory(ctx, "flow-1", "detect")
	if node == nil || node.Content != "node" {
		t.Error("node-scoped memory not found")
	}
}

func TestNodeMemory_Delete(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store, _ := NewSQLiteNodeMemoryStore(db)
	ctx := context.Background()

	store.UpsertMemory(ctx, &model.NodeMemory{FlowID: "flow-1", NodeID: "detect", Content: "d"})
	store.DeleteMemory(ctx, "flow-1", "detect")

	got, _ := store.GetMemory(ctx, "flow-1", "detect")
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestNodeMemory_Struct(t *testing.T) {
	m := &model.NodeMemory{FlowID: "f", NodeID: "n", Content: "c"}
	if m.FlowID != "f" || m.NodeID != "n" || m.Content != "c" {
		t.Error("NodeMemory struct init failed")
	}
}
