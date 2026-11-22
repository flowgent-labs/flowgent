package storage_test

// See sqlite_test.go for why this uses the external black-box package.

import (
	"context"
	"os"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	graph "github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	noderun "github.com/flowgent-labs/flowgent/storage/pkg/noderun"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPGDSN() string {
	if dsn := os.Getenv("TEST_PG_DSN"); dsn != "" {
		return dsn
	}
	return ""
}

func seedPostgresAgentGraph(t *testing.T, pool *pgxpool.Pool, namespace, name string) {
	t.Helper()
	if err := graph.NewAgentGraphPostgresStore(pool).SaveSpec(context.Background(), &entities.AgentGraph{Name: name,
		BaseEntity: entities.BaseEntity{ID: name, Namespace: namespace}, Kind: "graph",
	}, "test", "run fixture"); err != nil {
		t.Fatalf("seed graph %s/%s: %v", namespace, name, err)
	}
}

func TestPostgresPool_Init(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set, skipping Postgres integration test")
	}
	pool := storage.NewPostgresPool(context.Background(), dsn, "public")
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestPostgresStore_GraphRunCRUD(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	pool := storage.NewPostgresPool(context.Background(), dsn, "public")
	defer pool.Close()
	ctx := context.Background()
	s := graphrun.NewGraphRunPostgresStore(pool)
	seedPostgresAgentGraph(t, pool, "default", "pg-test-graph")

	run := &entities.GraphRun{
		BaseEntity:     entities.BaseEntity{Namespace: "default"},
		AgentGraphName: "pg-test-graph", AgentGraphRevision: 1, Status: entities.GraphRunPending,
	}
	run.SetTrigger(entities.TriggerInfo{Type: "manual", Source: "pg-ut"})
	if err := s.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentGraphName != "pg-test-graph" {
		t.Errorf("expected pg-test-graph, got %s", got.AgentGraphName)
	}

	run.Status = entities.GraphRunCompleted
	if err := s.Update(ctx, run); err != nil {
		t.Fatalf("Update: %v", err)
	}

	page, err := s.List(ctx, graphrun.ListFilter{Namespace: "default", Page: entities.PageRequest{Page: 1, Size: 100}})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount < 1 {
		t.Errorf("expected at least 1 run with empty filter, got %d", page.TotalCount)
	}
}

func TestPostgresStore_NodeRunCRUD(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	pool := storage.NewPostgresPool(context.Background(), dsn, "public")
	defer pool.Close()
	ctx := context.Background()
	frStore := graphrun.NewGraphRunPostgresStore(pool)
	tpStore := noderun.NewNodeRunPostgresStore(pool)
	seedPostgresAgentGraph(t, pool, "default", "f1")

	run := &entities.GraphRun{AgentGraphName: "f1", AgentGraphRevision: 1, Status: entities.GraphRunPending}
	frStore.Create(ctx, run)

	noderun := &entities.NodeRun{
		GraphRunID: run.ID, NodeInstanceID: "n1", Status: entities.NodeRunPending, ExecutionID: "pg-exec-1",
	}
	tpStore.CreateNodeRun(ctx, noderun)
	tpStore.UpdateNodeRun(ctx, noderun)

	_, err := tpStore.ListByGraphRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByGraphRun: %v", err)
	}
	_, err = tpStore.GetByExecutionID(ctx, "pg-exec-1")
	if err != nil {
		t.Fatalf("GetByExecutionID: %v", err)
	}
}
