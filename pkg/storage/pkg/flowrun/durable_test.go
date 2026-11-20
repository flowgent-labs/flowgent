package flowrun_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flow"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/task"
)

func durableFixture(t *testing.T) (*sql.DB, *entities.FlowRunInfo, *flowrun.SubflowStore) {
	t.Helper()
	ctx := context.Background()
	db := storage.NewSQLiteConn(ctx, t.TempDir())
	t.Cleanup(func() { db.Close() })
	repo := flow.NewFlowSQLiteStore(db)
	child := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "child", Namespace: "default"}, Nodes: []entities.Node{{ID: "seed", Kind: entities.NoopNode}}}
	if err := repo.CreateSpec(ctx, child, "test", "child"); err != nil {
		t.Fatal(err)
	}
	parent := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "parent", Namespace: "default"}, Nodes: []entities.Node{{ID: "repair", Kind: entities.AgentFlowNode, Subflow: &entities.SubflowConfig{Ref: "child", Mode: "adaptive", Supervisor: "planner", Mutation: entities.MutationPolicy{Allow: []string{"append", "update_pending_edges", "replace_pending", "seal"}}}}}}
	if err := repo.CreateSpec(ctx, parent, "test", "parent"); err != nil {
		t.Fatal(err)
	}
	run := &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{Namespace: "default"}, AgentFlowID: "parent"}
	if err := flowrun.NewFlowRunSQLiteStore(db).Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	store := flowrun.NewSubflowStore(db)
	if _, err := store.Transact(ctx, "default", run.ID, "repair", nil); err != nil {
		t.Fatal(err)
	}
	return db, run, store
}
func TestGraphCASAtomicEventAndOutbox(t *testing.T) {
	db, run, store := durableFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.Transact(ctx, "default", run.ID, "repair", &entities.GraphPatch{ID: fmt.Sprint(i), FlowRunID: run.ID, SubflowInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "append", Node: &entities.Node{ID: fmt.Sprintf("new%d", i), Kind: entities.NoopNode}}}})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, flowrun.ErrGraphConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	var events, outbox int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orh_event WHERE run_id=?`, run.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM orh_outbox WHERE aggregate_id=?`, run.ID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if events != 2 || outbox != 2 {
		t.Fatalf("event/outbox not atomic: %d/%d", events, outbox)
	}
	recovered, err := flowrun.NewSubflowStore(db).Transact(ctx, "default", run.ID, "repair", nil)
	if err != nil || recovered.Revision != 2 {
		t.Fatalf("recovery: %v %#v", err, recovered)
	}
}
func TestGraphDecisionReplayAndRollback(t *testing.T) {
	db, run, store := durableFixture(t)
	ctx := context.Background()
	patch := entities.GraphPatch{ID: "accepted", FlowRunID: run.ID, SubflowInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "append", Node: &entities.Node{ID: "test", Kind: entities.NoopNode}}}}
	first, err := store.Transact(ctx, "default", run.ID, "repair", &patch)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Transact(ctx, "default", run.ID, "repair", &patch)
	if err != nil || again.Revision != first.Revision {
		t.Fatal("committed decision was reapplied", err)
	}
	patch.Operations[0].Node.ID = "different"
	if _, err = store.Transact(ctx, "default", run.ID, "repair", &patch); !errors.Is(err, flowrun.ErrGraphConflict) {
		t.Fatal("same decision ID accepted with different content", err)
	}
	// Fail outbox insertion after the graph write; both graph and event must roll back.
	if _, err = db.Exec(`CREATE TRIGGER fail_outbox BEFORE INSERT ON orh_outbox BEGIN SELECT RAISE(ABORT,'injected crash before outbox'); END`); err != nil {
		t.Fatal(err)
	}
	patch.ID = "rollback"
	patch.ExpectedRevision = 2
	if _, err = store.Transact(ctx, "default", run.ID, "repair", &patch); err == nil {
		t.Fatal("injected transaction failure ignored")
	}
	got, err := store.Transact(ctx, "default", run.ID, "repair", nil)
	if err != nil || got.Revision != 2 {
		t.Fatal("partial graph commit", err)
	}
}
func TestDispatchedGraphMutationIsDeferred(t *testing.T) {
	db, run, store := durableFixture(t)
	ctx := context.Background()
	item := &entities.TaskRunInfo{BaseEntity: entities.BaseEntity{ID: "seed-attempt", Metadata: map[string]any{"node_spec": entities.NodeSpecFromNode(&entities.Node{ID: "seed", Kind: entities.NoopNode})}}, RunID: run.ID, NodeKey: "repair/seed", SubflowInstanceID: "repair", GraphRevision: 1, NodeInstanceID: "repair/seed", DispatchEpoch: 1, FencingToken: 1, Attempt: 1, Status: entities.TaskPending}
	if err := task.NewTaskSQLiteStore(db).Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	_, err := store.Transact(ctx, "default", run.ID, "repair", &entities.GraphPatch{ID: "unsafe", FlowRunID: run.ID, SubflowInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "replace_pending", NodeID: "seed", Node: &entities.Node{ID: "seed", Kind: entities.NoopNode}}}})
	if err == nil {
		t.Fatal("dispatched task was replaced")
	}
	got, _ := store.Transact(ctx, "default", run.ID, "repair", nil)
	if got.Revision != 1 {
		t.Fatal("deferred patch changed old state")
	}
}
func TestExecutionClaimTerminalAndOutboxRecovery(t *testing.T) {
	db, run, _ := durableFixture(t)
	ctx := context.Background()
	repo := task.NewTaskSQLiteStore(db)
	item := &entities.TaskRunInfo{BaseEntity: entities.BaseEntity{ID: "attempt-1", Metadata: map[string]any{"node_spec": &entities.NodeSpec{ID: "top", Kind: entities.NoopNode}}}, RunID: run.ID, NodeKey: "top", NodeInstanceID: "top", DispatchEpoch: 1, FencingToken: 1, Attempt: 1, Status: entities.TaskPending, ExecutionID: "plan-1-attempt-1"}
	if err := repo.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	plan := &entities.ExecutionPlan{PlanID: "plan-1", TaskID: item.ID, NodeID: "top", NodeInstanceID: "top", DispatchEpoch: 1, AgentFlowRunID: run.ID, AgentFlowDefinitionID: "parent", Namespace: "default", RuntimeClusterID: "cluster", TaskType: entities.TaskNoop, NodeSpec: &entities.NodeSpec{ID: "top", Kind: entities.NoopNode}}
	if err := task.DispatchExecution(ctx, db, plan); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		claimed, err := task.ClaimExecution(ctx, db, run.ID, item.ID, "top", "tm/slot", 1)
		if err != nil || claimed != (i == 0) {
			t.Fatalf("claim %d: %v %v", i, claimed, err)
		}
	}
	item.Status = entities.Success
	item.Output = map[string]any{"pr_id": "PR-42"}
	if err := repo.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, item); err != nil {
		t.Fatal("idempotent result rejected", err)
	}
	item.Output = map[string]any{"pr_id": "PR-43"}
	if err := repo.Save(ctx, item); err == nil {
		t.Fatal("terminal result mutated")
	}
	var published int
	for i := 0; i < 8; i++ {
		found, err := storage.PublishOutboxOnce(ctx, db, func(_ context.Context, topic string, payload []byte) error {
			if !json.Valid(payload) || topic == "" {
				t.Fatal("invalid transport envelope")
			}
			published++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	if published != 3 {
		t.Fatalf("recovered publications=%d, want init, dispatch, result", published)
	}
	var pending int
	db.QueryRow(`SELECT COUNT(*) FROM orh_outbox WHERE status='pending'`).Scan(&pending)
	if pending != 0 {
		t.Fatal("outbox not drained")
	}
	// A lost notification cannot hide the already committed business result.
	got, err := repo.Get(ctx, item.ID)
	if err != nil || got.Output["pr_id"] != "PR-42" {
		t.Fatal("durable result lost", err)
	}
}
func TestOutboxPublishFailureAndLeaseRecovery(t *testing.T) {
	db, _, _ := durableFixture(t)
	ctx := context.Background()
	found, err := storage.PublishOutboxOnce(ctx, db, func(context.Context, string, []byte) error { return errors.New("EMQX offline") })
	if !found || err == nil {
		t.Fatal("expected broker failure")
	}
	var pending, attempts int
	if err = db.QueryRow(`SELECT COUNT(*),MAX(attempts) FROM orh_outbox WHERE status='pending'`).Scan(&pending, &attempts); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || attempts != 1 {
		t.Fatal("publish failure lost event")
	}
	db.Exec(`UPDATE orh_outbox SET available_at=?`, time.Now().UTC().Add(-time.Second))
	found, err = storage.PublishOutboxOnce(ctx, db, func(context.Context, string, []byte) error { return nil })
	if !found || err != nil {
		t.Fatal("retry did not recover", err)
	}
}

func TestOutboxAggregateSequenceSurvivesClockSkew(t *testing.T) {
	db, _, _ := durableFixture(t)
	ctx := context.Background()
	err := storage.WithSQLTransaction(ctx, db, func(tx *storage.SQLTransaction) error {
		for i := int64(1); i <= 2; i++ {
			if err := storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: fmt.Sprintf("ordered-%d", i), Namespace: "default", AggregateID: "ordered", AggregateSeq: i, Type: "test", Topic: "test/ordered", Payload: json.RawMessage(fmt.Sprintf(`{"sequence":%d}`, i))}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A second API node's clock is far behind. Business sequence still wins.
	if _, err = db.Exec(`UPDATE orh_outbox SET created_at='2000-01-01 00:00:00' WHERE id='ordered-2'`); err != nil {
		t.Fatal(err)
	}
	var sequences []int
	for i := 0; i < 4; i++ {
		found, err := storage.PublishOutboxOnce(ctx, db, func(_ context.Context, topic string, payload []byte) error {
			if topic == "test/ordered" {
				var value struct {
					Sequence int `json:"sequence"`
				}
				if err := json.Unmarshal(payload, &value); err != nil {
					return err
				}
				sequences = append(sequences, value.Sequence)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	if len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 {
		t.Fatalf("aggregate reordered: %v", sequences)
	}
}

func TestOldReadySnapshotCannotAdmitAfterPatch(t *testing.T) {
	db, run, store := durableFixture(t)
	ctx := context.Background()
	patch := entities.GraphPatch{ID: "new-dependency", FlowRunID: run.ID, SubflowInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "append", Node: &entities.Node{ID: "prerequisite", Kind: entities.NoopNode}}, {Kind: "update_pending_edges", From: "prerequisite", To: "seed"}}}
	if _, err := store.Transact(ctx, "default", run.ID, "repair", &patch); err != nil {
		t.Fatal(err)
	}
	old := &entities.TaskRunInfo{BaseEntity: entities.BaseEntity{ID: "stale-ready", Metadata: map[string]any{"node_spec": entities.NodeSpecFromNode(&entities.Node{ID: "seed", Kind: entities.NoopNode})}}, RunID: run.ID, NodeKey: "repair/seed", SubflowInstanceID: "repair", GraphRevision: 1, NodeInstanceID: "repair/seed", DispatchEpoch: 1, FencingToken: 1, Attempt: 1, Status: entities.TaskPending}
	if err := task.NewTaskSQLiteStore(db).Save(ctx, old); err == nil {
		t.Fatal("stale Ready snapshot dispatched across a committed graph revision")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orh_node_run WHERE id=?`, old.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale admission wrote an execution fact", err)
	}
}
