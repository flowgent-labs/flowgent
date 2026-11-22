package storage_test

// NOTE: the external black-box package avoids an import cycle because entity
// repositories import their shared SQL implementation from package storage.
//
// This file exercises the modular per-entity SQLite stores directly — the
// monolithic Store type these tests originally targeted (NewSQLiteStore with
// CreateGraphRun/GetGraphRun/... methods) was removed when the codebase moved
// to one repository implementation per entity under storage/pkg/{graph,graphrun,
// noderun,approval,...} (see "Enforce apiserver-only DB access pattern"
// refactor).

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	graph "github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
	"github.com/flowgent-labs/flowgent/storage/pkg/approval"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	noderun "github.com/flowgent-labs/flowgent/storage/pkg/noderun"
)

func TestSQLiteConn_Init(t *testing.T) {
	conn := storage.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func seedSQLiteAgentGraph(t *testing.T, conn *sql.DB, namespace, name string) {
	t.Helper()
	if err := graph.NewAgentGraphSQLiteStore(conn).CreateSpec(context.Background(), &entities.AgentGraph{Name: name,
		BaseEntity: entities.BaseEntity{ID: name, Namespace: namespace}, Kind: "graph",
	}, "test", "run fixture"); err != nil {
		t.Fatalf("seed graph %s/%s: %v", namespace, name, err)
	}
}

func TestSQLiteStore_GraphRunCRUD(t *testing.T) {
	conn := storage.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	s := graphrun.NewGraphRunSQLiteStore(conn)
	seedSQLiteAgentGraph(t, conn, "default", "test-graph")

	run := &entities.GraphRun{
		BaseEntity:     entities.BaseEntity{Namespace: "default"},
		AgentGraphName: "test-graph", AgentGraphRevision: 1, Status: entities.GraphRunPending,
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
	if got.AgentGraphName != "test-graph" {
		t.Errorf("expected test-graph, got %s", got.AgentGraphName)
	}

	run.Status = entities.GraphRunRunning
	if err := s.Update(ctx, run); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = s.Get(ctx, run.ID)
	if got.Status != entities.GraphRunRunning {
		t.Errorf("expected RUNNING after update, got %s", got.Status)
	}

	page, err := s.List(ctx, graphrun.ListFilter{Namespace: "default", Page: entities.PageRequest{Page: 1, Size: 100}})
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
	if got.Status != entities.GraphRunCancelled {
		t.Errorf("expected CANCELLED after Cancel, got %s", got.Status)
	}

	if err := s.Delete(ctx, run.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, run.ID); err == nil {
		t.Fatalf("Get returned soft-deleted run")
	}
	page, err = s.List(ctx, graphrun.ListFilter{Namespace: "default", Page: entities.PageRequest{Page: 1, Size: 100}})
	if err != nil {
		t.Fatalf("Select after delete: %v", err)
	}
	if page.TotalCount != 0 {
		t.Fatalf("expected no active runs after delete, got %d", page.TotalCount)
	}
}

func TestSQLiteStore_NodeRunCRUD(t *testing.T) {
	conn := storage.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	frStore := graphrun.NewGraphRunSQLiteStore(conn)
	tpStore := noderun.NewNodeRunSQLiteStore(conn)
	seedSQLiteAgentGraph(t, conn, "default", "f1")

	run := &entities.GraphRun{AgentGraphName: "f1", AgentGraphRevision: 1, Status: entities.GraphRunPending}
	if err := frStore.Create(ctx, run); err != nil {
		t.Fatalf("Create graph run: %v", err)
	}

	noderun := &entities.NodeRun{
		BaseEntity: entities.BaseEntity{ID: "noderun-run-stable-id"},
		GraphRunID: run.ID, NodeInstanceID: "node-1", Status: entities.NodeRunPending,
		ExecutionID: "exec-001", MaxRetries: 3, StartedAt: ptrTime(time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)),
	}
	if err := tpStore.CreateNodeRun(ctx, noderun); err != nil {
		t.Fatalf("CreateNodeRun: %v", err)
	}
	if noderun.ID != "noderun-run-stable-id" {
		t.Fatalf("caller-provided NodeRun ID was replaced: %q", noderun.ID)
	}

	got, err := tpStore.Get(ctx, noderun.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NodeInstanceID != "node-1" {
		t.Errorf("expected node-1, got %s", got.NodeInstanceID)
	}
	if got.StartedAt == nil || got.StartedAt.Year() != 2026 {
		t.Fatalf("SQLite NodeRun started_at was not decoded: %v", got.StartedAt)
	}

	noderun.Status = entities.NodeRunSucceeded
	noderun.Output = map[string]any{"result": "ok"}
	if err := tpStore.UpdateNodeRun(ctx, noderun); err != nil {
		t.Fatalf("UpdateNodeRun: %v", err)
	}

	tasks, err := tpStore.ListByGraphRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByGraphRun: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 noderun, got %d", len(tasks))
	}

	gotByExec, err := tpStore.GetByExecutionID(ctx, "exec-001")
	if err != nil {
		t.Fatalf("GetByExecutionID: %v", err)
	}
	if gotByExec.ExecutionID != "exec-001" {
		t.Errorf("expected exec-001, got %s", gotByExec.ExecutionID)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

func TestSQLiteStore_HumanApprovalCRUD(t *testing.T) {
	conn := storage.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	frStore := graphrun.NewGraphRunSQLiteStore(conn)
	tpStore := noderun.NewNodeRunSQLiteStore(conn)
	apStore := approval.NewApprovalSQLiteStore(conn)
	seedSQLiteAgentGraph(t, conn, "default", "f1")

	run := &entities.GraphRun{AgentGraphName: "f1", AgentGraphRevision: 1, Status: entities.GraphRunPending}
	if err := frStore.Create(ctx, run); err != nil {
		t.Fatalf("Create graph run: %v", err)
	}
	noderun := &entities.NodeRun{GraphRunID: run.ID, NodeInstanceID: "human", Status: entities.NodeRunPending, ExecutionID: "e1"}
	if err := tpStore.CreateNodeRun(ctx, noderun); err != nil {
		t.Fatalf("Create noderun run: %v", err)
	}

	appr := &entities.ApprovalInfo{
		NodeRunID: noderun.ID, GraphRunID: run.ID, Status: "PENDING", Timeout: 1 * time.Hour,
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
	if got.Status != "pending" {
		t.Errorf("expected pending, got %s", got.Status)
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

func TestSQLiteStore_AgentGraphCRUD(t *testing.T) {
	conn := storage.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	s := graph.NewAgentGraphSQLiteStore(conn)

	const namespace = "team-a"
	if err := s.SaveSpec(ctx, &entities.AgentGraph{Name: "graph-1",
		BaseEntity: entities.BaseEntity{ID: "graph-1", Namespace: namespace},
	}, "test", "initial"); err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}

	got, err := s.Get(ctx, namespace, "graph-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Revision != 1 {
		t.Errorf("expected v1, got %d", got.Revision)
	}

	gotVer, err := s.GetVersion(ctx, namespace, "graph-1", 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if gotVer.AgentGraphName != "graph-1" || gotVer.AgentGraphID == "graph-1" {
		t.Errorf("expected stable ID plus name graph-1, got id=%s name=%s", gotVer.AgentGraphID, gotVer.AgentGraphName)
	}

	page, err := s.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount < 1 {
		t.Errorf("expected at least 1 def, got %d", page.TotalCount)
	}

	if err := s.SaveSpec(ctx, &entities.AgentGraph{Name: "graph-1",
		BaseEntity: entities.BaseEntity{ID: "graph-1", Namespace: "team-b"},
	}, "test", "same ID in another namespace"); err != nil {
		t.Fatalf("SaveSpec cross namespace: %v", err)
	}
	if _, err := s.GetSpec(ctx, "team-b", "graph-1"); err != nil {
		t.Fatalf("GetSpec cross namespace: %v", err)
	}

	if err := s.Delete(ctx, namespace, "graph-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, namespace, "graph-1"); err == nil {
		t.Fatalf("Get returned soft-deleted graph")
	}
	if _, err := s.GetVersion(ctx, namespace, "graph-1", 1); err == nil {
		t.Fatalf("GetVersion returned soft-deleted graph")
	}
	if _, err := s.GetSpec(ctx, namespace, "graph-1"); err == nil {
		t.Fatalf("GetSpec returned soft-deleted graph")
	}
	page, err = s.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 100})
	if err != nil {
		t.Fatalf("Select after delete: %v", err)
	}
	if page.TotalCount != 0 {
		t.Fatalf("expected no active defs after delete, got %d", page.TotalCount)
	}

	if err := s.SaveSpec(ctx, &entities.AgentGraph{Name: "graph-1",
		BaseEntity: entities.BaseEntity{ID: "graph-1", Namespace: namespace},
	}, "test", "restore"); err != nil {
		t.Fatalf("SaveSpec restore: %v", err)
	}
	if _, err := s.GetSpec(ctx, namespace, "graph-1"); err != nil {
		t.Fatalf("GetSpec after restore: %v", err)
	}
	if _, err := s.GetSpec(ctx, "team-b", "graph-1"); err != nil {
		t.Fatalf("other namespace was affected by delete/restore: %v", err)
	}
}

func TestSQLiteStore_AgentGraphNameContract(t *testing.T) {
	conn := storage.NewSQLiteConn(context.Background(), t.TempDir())
	defer conn.Close()
	ctx := context.Background()
	s := graph.NewAgentGraphSQLiteStore(conn)

	first := &entities.AgentGraph{Name: "Security_Fixer",
		BaseEntity: entities.BaseEntity{ID: "Security_Fixer", Namespace: "team-a"},
		Kind:       "graph",
	}
	if err := s.CreateSpec(ctx, first, "test", "create"); err != nil {
		t.Fatalf("CreateSpec: %v", err)
	}
	if err := s.CreateSpec(ctx, first, "test", "duplicate"); !errors.Is(err, graph.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateSpec error = %v, want ErrAlreadyExists", err)
	}
	caseVariant := &entities.AgentGraph{Name: "security_fixer",
		BaseEntity: entities.BaseEntity{ID: "security_fixer", Namespace: "team-a"},
		Kind:       "graph",
	}
	if err := s.CreateSpec(ctx, caseVariant, "test", "case duplicate"); !errors.Is(err, graph.ErrAlreadyExists) {
		t.Fatalf("case-insensitive duplicate error = %v, want ErrAlreadyExists", err)
	}
	caseVariant.Namespace = "team-b"
	if err := s.CreateSpec(ctx, caseVariant, "test", "other namespace"); err != nil {
		t.Fatalf("same name in another namespace: %v", err)
	}
	if err := s.SaveSpec(ctx, &entities.AgentGraph{Name: "1-invalid",
		BaseEntity: entities.BaseEntity{ID: "1-invalid", Namespace: "team-a"}, Kind: "graph",
	}, "test", "invalid"); err == nil {
		t.Fatal("SaveSpec accepted an invalid graph name")
	}
}
