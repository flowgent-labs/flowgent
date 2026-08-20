package store_test

// NOTE: package store_test (external/black-box test package) is required
// here, not package store: the per-entity stores below (agentflow, flowrun,
// task, approval) all import store.SQLiteGenericStore from the parent
// "store" package, so an in-package test here that also imported them would
// create an import cycle (store -> flowrun -> store).
//
// This file exercises the modular per-entity SQLite stores directly — the
// monolithic Store type these tests originally targeted (NewSQLiteStore with
// CreateFlowRun/GetFlowRun/... methods) was removed when the codebase moved
// to one store implementation per entity under store/pkg/{agentflow,flowrun,
// task,approval,...} (see "Enforce apiserver-only DB access pattern"
// refactor).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	store "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/approval"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/task"
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
		BaseEntity:  entities.BaseEntity{Namespace: "default"},
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

	page, err := s.List(ctx, flowrun.ListFilter{Namespace: "default", Page: entities.PageRequest{Page: 1, Size: 100}})
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

	if err := s.Delete(ctx, run.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, run.ID); err == nil {
		t.Fatalf("Get returned soft-deleted run")
	}
	page, err = s.List(ctx, flowrun.ListFilter{Namespace: "default", Page: entities.PageRequest{Page: 1, Size: 100}})
	if err != nil {
		t.Fatalf("Select after delete: %v", err)
	}
	if page.TotalCount != 0 {
		t.Fatalf("expected no active runs after delete, got %d", page.TotalCount)
	}
}

func TestSQLiteStore_TaskRunCRUD(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	frStore := flowrun.NewFlowRunSQLiteStore(conn)
	tpStore := task.NewTaskSQLiteStore(conn)

	run := &entities.FlowRunInfo{AgentFlowID: "f1", Version: 1, Status: entities.RunPending}
	if err := frStore.Create(ctx, run); err != nil {
		t.Fatalf("Create flow run: %v", err)
	}

	task := &entities.TaskRunInfo{
		BaseEntity:     entities.BaseEntity{ID: "task-run-stable-id"},
		AgentFlowRunID: run.ID, NodeID: "node-1", Status: entities.TaskPending,
		ExecID: "exec-001", MaxRetries: 3, StartedAt: ptrTime(time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)),
	}
	if err := tpStore.CreateTaskRun(ctx, task); err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if task.ID != "task-run-stable-id" {
		t.Fatalf("caller-provided TaskRun ID was replaced: %q", task.ID)
	}

	got, err := tpStore.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", got.NodeID)
	}
	if got.StartedAt == nil || got.StartedAt.Year() != 2026 {
		t.Fatalf("SQLite TaskRun started_at was not decoded: %v", got.StartedAt)
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

func ptrTime(value time.Time) *time.Time { return &value }

func TestSQLiteStore_HumanApprovalCRUD(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	frStore := flowrun.NewFlowRunSQLiteStore(conn)
	tpStore := task.NewTaskSQLiteStore(conn)
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

	pendingBefore, err := apStore.ListPending(ctx, appr.Namespace)
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

	pending, err := apStore.ListPending(ctx, appr.Namespace)
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
	s := flow.NewFlowSQLiteStore(conn)

	const namespace = "team-a"
	if err := s.SaveSpec(ctx, &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "flow-1", Namespace: namespace},
	}, "test", "initial"); err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}

	got, err := s.Get(ctx, namespace, "flow-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("expected v1, got %d", got.Version)
	}

	gotVer, err := s.GetVersion(ctx, namespace, "flow-1", 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if gotVer.FlowID != "flow-1" {
		t.Errorf("expected flow-1, got %s", gotVer.FlowID)
	}

	page, err := s.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount < 1 {
		t.Errorf("expected at least 1 def, got %d", page.TotalCount)
	}

	if err := s.SaveSpec(ctx, &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "flow-1", Namespace: "team-b"},
	}, "test", "same ID in another namespace"); err != nil {
		t.Fatalf("SaveSpec cross namespace: %v", err)
	}
	if _, err := s.GetSpec(ctx, "team-b", "flow-1"); err != nil {
		t.Fatalf("GetSpec cross namespace: %v", err)
	}

	if err := s.Delete(ctx, namespace, "flow-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, namespace, "flow-1"); err == nil {
		t.Fatalf("Get returned soft-deleted flow")
	}
	if _, err := s.GetVersion(ctx, namespace, "flow-1", 1); err == nil {
		t.Fatalf("GetVersion returned soft-deleted flow")
	}
	if _, err := s.GetSpec(ctx, namespace, "flow-1"); err == nil {
		t.Fatalf("GetSpec returned soft-deleted flow")
	}
	page, err = s.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("Select after delete: %v", err)
	}
	if page.TotalCount != 0 {
		t.Fatalf("expected no active defs after delete, got %d", page.TotalCount)
	}

	if err := s.SaveSpec(ctx, &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "flow-1", Namespace: namespace},
	}, "test", "restore"); err != nil {
		t.Fatalf("SaveSpec restore: %v", err)
	}
	if _, err := s.GetSpec(ctx, namespace, "flow-1"); err != nil {
		t.Fatalf("GetSpec after restore: %v", err)
	}
	if _, err := s.GetSpec(ctx, "team-b", "flow-1"); err != nil {
		t.Fatalf("other namespace was affected by delete/restore: %v", err)
	}
}

func TestSQLiteStore_FlowNameContract(t *testing.T) {
	conn := store.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	s := flow.NewFlowSQLiteStore(conn)

	first := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "Security_Fixer", Namespace: "team-a"},
		Kind:       "flow",
	}
	if err := s.CreateSpec(ctx, first, "test", "create"); err != nil {
		t.Fatalf("CreateSpec: %v", err)
	}
	if err := s.CreateSpec(ctx, first, "test", "duplicate"); !errors.Is(err, flow.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateSpec error = %v, want ErrAlreadyExists", err)
	}
	caseVariant := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "security_fixer", Namespace: "team-a"},
		Kind:       "flow",
	}
	if err := s.CreateSpec(ctx, caseVariant, "test", "case duplicate"); !errors.Is(err, flow.ErrAlreadyExists) {
		t.Fatalf("case-insensitive duplicate error = %v, want ErrAlreadyExists", err)
	}
	caseVariant.Namespace = "team-b"
	if err := s.CreateSpec(ctx, caseVariant, "test", "other namespace"); err != nil {
		t.Fatalf("same name in another namespace: %v", err)
	}
	if err := s.SaveSpec(ctx, &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "1-invalid", Namespace: "team-a"}, Kind: "flow",
	}, "test", "invalid"); err == nil {
		t.Fatal("SaveSpec accepted an invalid flow name")
	}
}
