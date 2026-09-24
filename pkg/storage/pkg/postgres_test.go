package storage_test

// See sqlite_test.go for why this uses the external black-box package.

import (
	"context"
	"os"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flow"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/task"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPGDSN() string {
	if dsn := os.Getenv("TEST_PG_DSN"); dsn != "" {
		return dsn
	}
	return ""
}

func seedPostgresFlow(t *testing.T, pool *pgxpool.Pool, namespace, name string) {
	t.Helper()
	if err := flow.NewFlowPostgresStore(pool).SaveSpec(context.Background(), &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: name, Namespace: namespace}, Kind: "flow",
	}, "test", "run fixture"); err != nil {
		t.Fatalf("seed flow %s/%s: %v", namespace, name, err)
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

func TestPostgresStore_FlowRunCRUD(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	pool := storage.NewPostgresPool(context.Background(), dsn, "public")
	defer pool.Close()
	ctx := context.Background()
	s := flowrun.NewFlowRunPostgresStore(pool)
	seedPostgresFlow(t, pool, "default", "pg-test-flow")

	run := &entities.FlowRunInfo{
		BaseEntity:  entities.BaseEntity{Namespace: "default"},
		AgentFlowID: "pg-test-flow", Version: 1, Status: entities.RunPending,
	}
	run.SetTrigger(entities.TriggerInfo{Type: "manual", Source: "pg-ut"})
	if err := s.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentFlowID != "pg-test-flow" {
		t.Errorf("expected pg-test-flow, got %s", got.AgentFlowID)
	}

	run.Status = entities.RunCompleted
	if err := s.Update(ctx, run); err != nil {
		t.Fatalf("Update: %v", err)
	}

	page, err := s.List(ctx, flowrun.ListFilter{Namespace: "default", Page: entities.PageRequest{Page: 1, Size: 100}})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount < 1 {
		t.Errorf("expected at least 1 run with empty filter, got %d", page.TotalCount)
	}
}

func TestPostgresStore_TaskRunCRUD(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	pool := storage.NewPostgresPool(context.Background(), dsn, "public")
	defer pool.Close()
	ctx := context.Background()
	frStore := flowrun.NewFlowRunPostgresStore(pool)
	tpStore := task.NewTaskPostgresStore(pool)
	seedPostgresFlow(t, pool, "default", "f1")

	run := &entities.FlowRunInfo{AgentFlowID: "f1", Version: 1, Status: entities.RunPending}
	frStore.Create(ctx, run)

	task := &entities.TaskRunInfo{
		AgentFlowRunID: run.ID, NodeID: "n1", Status: entities.TaskPending, ExecID: "pg-exec-1",
	}
	tpStore.CreateTaskRun(ctx, task)
	tpStore.UpdateTaskRun(ctx, task)

	_, err := tpStore.ListByFlowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByFlowRun: %v", err)
	}
	_, err = tpStore.GetByExecID(ctx, "pg-exec-1")
	if err != nil {
		t.Fatalf("GetByExecID: %v", err)
	}
}
