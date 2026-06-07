package store

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

func TestSQLiteStore_Init(t *testing.T) {
	s := NewSQLiteStore(t.TempDir())
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Fatal("DB should not be nil")
	}
}

func TestSQLiteStore_AgentFlowRuns(t *testing.T) {
	s := NewSQLiteStore(t.TempDir())
	s.Init(context.Background())
	defer s.Close()
	ctx := context.Background()

	run := &model.AgentFlowRun{
		AgentFlowID: "test-flow", Version: 1, Status: model.RunPending,
		Trigger: model.TriggerInfo{Type: "manual", Source: "ut"},
	}
	if err := s.CreateFlowRun(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.ID == "" {
		t.Fatal("ID should be set")
	}

	got, err := s.GetFlowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentFlowID != "test-flow" {
		t.Errorf("expected test-flow, got %s", got.AgentFlowID)
	}

	run.Status = model.RunRunning
	if err := s.UpdateFlowRun(ctx, run); err != nil {
		t.Fatalf("Update: %v", err)
	}

	runs, err := s.ListFlowRuns(ctx, "test-flow", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}

	// Empty agentFlowID should list all
	allRuns, _ := s.ListFlowRuns(ctx, "", 100)
	if len(allRuns) < 1 {
		t.Errorf("expected at least 1 run, got %d", len(allRuns))
	}
}

func TestSQLiteStore_TaskRuns(t *testing.T) {
	s := NewSQLiteStore(t.TempDir())
	s.Init(context.Background())
	defer s.Close()
	ctx := context.Background()

	// Create a parent run first
	run := &model.AgentFlowRun{AgentFlowID: "f1", Version: 1, Status: model.RunPending}
	s.CreateFlowRun(ctx, run)

	task := &model.TaskRun{
		AgentFlowRunID: run.ID, NodeID: "node-1", Status: model.TaskPending,
		ExecID: "exec-001", MaxRetries: 3,
	}
	if err := s.CreateTaskRun(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == "" {
		t.Fatal("ID should be set")
	}

	got, err := s.GetTaskRun(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", got.NodeID)
	}

	task.Status = model.Success
	task.Output = map[string]any{"result": "ok"}
	s.UpdateTaskRun(ctx, task)

	tasks, err := s.ListTaskRunsByFlow(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetTaskRuns: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	gotByExec, err := s.GetTaskRunByExecID(ctx, "exec-001")
	if err != nil {
		t.Fatalf("GetByExecID: %v", err)
	}
	if gotByExec.ExecID != "exec-001" {
		t.Errorf("expected exec-001, got %s", gotByExec.ExecID)
	}
}

func TestSQLiteStore_HumanApproval(t *testing.T) {
	s := NewSQLiteStore(t.TempDir())
	s.Init(context.Background())
	defer s.Close()
	ctx := context.Background()

	run := &model.AgentFlowRun{AgentFlowID: "f1", Version: 1, Status: model.RunPending}
	s.CreateFlowRun(ctx, run)
	task := &model.TaskRun{AgentFlowRunID: run.ID, NodeID: "human", Status: model.TaskPending, ExecID: "e1"}
	s.CreateTaskRun(ctx, task)

	approval := &model.HumanApproval{
		TaskRunID: task.ID, Status: "PENDING", Timeout: 1 * time.Hour,
	}
	if err := s.CreateApproval(ctx, approval); err != nil {
		t.Fatalf("CreateHuman: %v", err)
	}
	if approval.Token == "" {
		t.Fatal("Token should be set")
	}

	got, err := s.GetApproval(ctx, approval.Token)
	if err != nil {
		t.Fatalf("GetHuman: %v", err)
	}
	if got.Status != "PENDING" {
		t.Errorf("expected PENDING, got %s", got.Status)
	}

	approved := true
	approval.Approved = &approved
	approval.Status = "APPROVED"
	s.UpdateApproval(ctx, approval)

	pending, _ := s.ListPendingApprovals(ctx)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestSQLiteStore_AgentFlowDefinitions(t *testing.T) {
	s := NewSQLiteStore(t.TempDir())
	s.Init(context.Background())
	defer s.Close()
	ctx := context.Background()

	def := &model.AgentFlowVersion{
		AgentFlowID: "flow-1", Version: 1, Definition: []byte(`{"id":"flow-1"}`),
		CreatedBy: "test", Comment: "initial",
	}
	s.SaveAgentFlow(ctx, def)

	latest, err := s.GetAgentFlow(ctx, "flow-1")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if latest.Version != 1 {
		t.Errorf("expected v1, got %d", latest.Version)
	}

	got, err := s.GetAgentFlowVersion(ctx, "flow-1", 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentFlowID != "flow-1" {
		t.Errorf("expected flow-1, got %s", got.AgentFlowID)
	}

	all, _ := s.ListAgentFlows(ctx)
	if len(all) < 1 {
		t.Errorf("expected at least 1 def, got %d", len(all))
	}
}
