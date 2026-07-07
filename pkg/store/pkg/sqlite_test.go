package store_test

// NOTE: package store_test (external/black-box test package) is required
// here, not package store: the per-entity stores below (agentflow, flowrun,
// taskplan, approval) all import store.SQLiteGenericStore from the parent
// "store" package, so an in-package test here that also imported them would
// create an import cycle (store -> flowrun -> store).
//
// This file exercises the modular per-entity SQLite stores directly — the
// monolithic Store type these tests originally targeted (NewSQLiteStore with
// CreateFlowRun/GetFlowRun/... methods) was removed when the codebase moved
// to one store implementation per entity under store/pkg/{agentflow,flowrun,
// taskplan,approval,...} (see "Enforce apiserver-only DB access pattern"
// refactor).

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	store "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentflow"
	"github.com/flowgent-labs/flowgent/store/pkg/approval"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/taskplan"
)

func TestSQLiteConn_Init(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestSQLiteStore_FlowRunCRUD(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	s := flowrun.NewFlowRunSQLiteStore(conn)

	run := &entities.FlowRunInfo{
		AgentFlowID: "test-flow", Version: 1, Status: entities.RunPending,
	}
	run.SetTrigger(entities.TriggerInfo{Type: "manual", Source: "ut"})
	if err := s.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.ID == "" {
		t.Fatal("ID should be set")
	}

	got, err := s.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentFlowID != "test-flow" {
		t.Errorf("expected test-flow, got %s", got.AgentFlowID)
	}

	run.Status = entities.RunRunning
	if err := s.Update(ctx, run); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = s.Get(ctx, run.ID)
	if got.Status != entities.RunRunning {
		t.Errorf("expected RUNNING after update, got %s", got.Status)
	}

	page, err := s.Select(ctx, entities.PageRequest{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount < 1 {
		t.Errorf("expected at least 1 run, got %d", page.TotalCount)
	}

	if err := s.Cancel(ctx, run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, _ = s.Get(ctx, run.ID)
	if got.Status != entities.RunCancelled {
		t.Errorf("expected CANCELLED after Cancel, got %s", got.Status)
	}
}

func TestSQLiteStore_TaskRunCRUD(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	frStore := flowrun.NewFlowRunSQLiteStore(conn)
	tpStore := taskplan.NewTaskPlanSQLiteStore(conn)

	run := &entities.FlowRunInfo{AgentFlowID: "f1", Version: 1, Status: entities.RunPending}
	if err := frStore.Create(ctx, run); err != nil {
		t.Fatalf("Create flow run: %v", err)
	}

	task := &entities.TaskRunInfo{
		AgentFlowRunID: run.ID, NodeID: "node-1", Status: entities.TaskPending,
		ExecID: "exec-001", MaxRetries: 3,
	}
	if err := tpStore.CreateTaskRun(ctx, task); err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if task.ID == "" {
		t.Fatal("ID should be set")
	}

	got, err := tpStore.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", got.NodeID)
	}

	task.Status = entities.Success
	task.Output = map[string]any{"result": "ok"}
	if err := tpStore.UpdateTaskRun(ctx, task); err != nil {
		t.Fatalf("UpdateTaskRun: %v", err)
	}

	tasks, err := tpStore.ListByFlowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByFlowRun: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	gotByExec, err := tpStore.GetByExecID(ctx, "exec-001")
	if err != nil {
		t.Fatalf("GetByExecID: %v", err)
	}
	if gotByExec.ExecID != "exec-001" {
		t.Errorf("expected exec-001, got %s", gotByExec.ExecID)
	}
}

func TestSQLiteStore_HumanApprovalCRUD(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	frStore := flowrun.NewFlowRunSQLiteStore(conn)
	tpStore := taskplan.NewTaskPlanSQLiteStore(conn)
	apStore := approval.NewApprovalSQLiteStore(conn)

	run := &entities.FlowRunInfo{AgentFlowID: "f1", Version: 1, Status: entities.RunPending}
	frStore.Create(ctx, run)
	task := &entities.TaskRunInfo{AgentFlowRunID: run.ID, NodeID: "human", Status: entities.TaskPending, ExecID: "e1"}
	tpStore.CreateTaskRun(ctx, task)

	appr := &entities.ApprovalInfo{
		TaskRunID: task.ID, AgentFlowRunID: run.ID, Status: "PENDING", Timeout: 1 * time.Hour,
	}
	if err := apStore.CreateApproval(ctx, appr); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if appr.Token == "" {
		t.Fatal("Token should be set")
	}

	got, err := apStore.Get(ctx, appr.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "PENDING" {
		t.Errorf("expected PENDING, got %s", got.Status)
	}

	pendingBefore, err := apStore.ListPending(ctx)
	if err != nil {
		t.Fatalf("ListPending (before approve): %v", err)
	}
	if len(pendingBefore) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pendingBefore))
	}

	approved := true
	appr.Approved = &approved
	appr.Status = "APPROVED"
	if err := apStore.UpdateApproval(ctx, appr); err != nil {
		t.Fatalf("UpdateApproval: %v", err)
	}

	pending, err := apStore.ListPending(ctx)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestSQLiteStore_AgentFlowDefinitionCRUD(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	s := agentflow.NewAgentFlowSQLiteStore(conn)

	def := &entities.AgentFlowVersionInfo{
		BaseEntity:  entities.BaseEntity{CreatedBy: "test"},
		AgentFlowID: "flow-1", Version: 1, Definition: []byte(`{"id":"flow-1"}`),
		Comment: "initial",
	}
	if err := s.Save(ctx, def); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, "flow-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("expected v1, got %d", got.Version)
	}

	gotVer, err := s.GetVersion(ctx, "flow-1", 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if gotVer.AgentFlowID != "flow-1" {
		t.Errorf("expected flow-1, got %s", gotVer.AgentFlowID)
	}

	page, err := s.Select(ctx, entities.PageRequest{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount < 1 {
		t.Errorf("expected at least 1 def, got %d", page.TotalCount)
	}
}
