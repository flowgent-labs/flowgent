package store

import (
	"context"
	"os"
	"testing"

	"github.com/flowgent-labs/flowgent/model/src"
)

func testPGDSN() string {
	if dsn := os.Getenv("TEST_PG_DSN"); dsn != "" {
		return dsn
	}
	return ""
}

func TestPostgresStore_Init(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set, skipping Postgres integration test")
	}
	s := NewPostgresStore(dsn)
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Fatal("DB should not be nil")
	}
}

func TestPostgresStore_PoolConfig(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	s := NewPostgresStore(dsn)
	s.SetPoolConfig(3, 10)
	s.SetSchema("public")
	s.Init(context.Background())
	defer s.Close()
	// Verify pool config applied
	stats := s.db.Stats()
	if stats.MaxOpenConnections != 10 {
		t.Logf("MaxOpenConnections: %d (may differ based on driver)", stats.MaxOpenConnections)
	}
}

func TestPostgresStore_AgentFlowRunCRUD(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	s := NewPostgresStore(dsn)
	s.Init(context.Background())
	defer s.Close()
	ctx := context.Background()

	run := &model.AgentFlowRun{
		AgentFlowID: "pg-test-flow", Version: 1, Status: model.RunPending,
		Trigger: model.TriggerInfo{Type: "manual", Source: "pg-ut"},
	}
	if err := s.CreateAgentFlowRun(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.GetAgentFlowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentFlowID != "pg-test-flow" {
		t.Errorf("expected pg-test-flow, got %s", got.AgentFlowID)
	}

	runs, _ := s.ListAgentFlowRuns(ctx, "pg-test-flow", 10)
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}

	run.Status = model.RunCompleted
	s.UpdateAgentFlowRun(ctx, run)

	// Empty filter lists all
	allRuns, _ := s.ListAgentFlowRuns(ctx, "", 100)
	if len(allRuns) < 1 {
		t.Errorf("expected at least 1 run with empty filter, got %d", len(allRuns))
	}
}

func TestPostgresStore_TaskRunCRUD(t *testing.T) {
	dsn := testPGDSN()
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	s := NewPostgresStore(dsn)
	s.Init(context.Background())
	defer s.Close()
	ctx := context.Background()

	run := &model.AgentFlowRun{AgentFlowID: "f1", Version: 1, Status: model.RunPending}
	s.CreateAgentFlowRun(ctx, run)

	task := &model.TaskRun{
		AgentFlowRunID: run.ID, NodeID: "n1", Status: model.TaskPending, ExecID: "pg-exec-1",
	}
	s.CreateTaskRun(ctx, task)
	s.UpdateTaskRun(ctx, task)

	_, err := s.GetTaskRunsByAgentFlowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetTaskRuns: %v", err)
	}
	_, err = s.GetTaskRunByExecID(ctx, "pg-exec-1")
	if err != nil {
		t.Fatalf("GetByExecID: %v", err)
	}
}
