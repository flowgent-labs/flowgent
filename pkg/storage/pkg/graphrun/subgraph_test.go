package graphrun_test

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
	"github.com/flowgent-labs/flowgent/storage/pkg/agent"
	graph "github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	noderun "github.com/flowgent-labs/flowgent/storage/pkg/noderun"
)

func durableFixture(t *testing.T) (*sql.DB, *entities.GraphRun, *graphrun.SubGraphStore) {
	t.Helper()
	ctx := context.Background()
	db := storage.NewSQLiteConn(ctx, t.TempDir())
	t.Cleanup(func() { db.Close() })
	repo := graph.NewAgentGraphSQLiteStore(db)
	child := &entities.AgentGraph{Name: "child", BaseEntity: entities.BaseEntity{ID: "child", Namespace: "default"}, Nodes: []entities.GraphNode{{ID: "seed", Kind: entities.NoopNode}}}
	if err := repo.CreateSpec(ctx, child, "test", "child"); err != nil {
		t.Fatal(err)
	}
	parent := &entities.AgentGraph{Name: "parent", BaseEntity: entities.BaseEntity{ID: "parent", Namespace: "default"}, Nodes: []entities.GraphNode{{ID: "repair", Kind: entities.SubGraphNode, SubGraph: &entities.SubGraphConfig{Ref: "child", Mode: "adaptive", Supervisor: "planner", Mutation: entities.MutationPolicy{Allow: []string{"append", "update_pending_edges", "replace_pending", "seal"}}}}}}
	if err := repo.CreateSpec(ctx, parent, "test", "parent"); err != nil {
		t.Fatal(err)
	}
	if err := agent.NewAgentSQLiteStore(db).Save(ctx, &entities.AgentInfo{BaseEntity: entities.BaseEntity{Namespace: "default"}, Name: "planner", Model: "fixture"}); err != nil {
		t.Fatal(err)
	}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{Namespace: "default"}, AgentGraphName: "parent"}
	if err := graphrun.NewGraphRunSQLiteStore(db).Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.PublishOutboxOnce(ctx, db, func(context.Context, string, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	store := graphrun.NewSubGraphStore(db)
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
			_, err := store.Transact(ctx, "default", run.ID, "repair", &entities.GraphPatch{ID: fmt.Sprint(i), GraphRunID: run.ID, SubGraphInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "append", Node: &entities.GraphNode{ID: fmt.Sprintf("new%d", i), Kind: entities.NoopNode}}}})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, graphrun.ErrGraphConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	var events, outbox int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orh_event WHERE graph_run_id=?`, run.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM orh_outbox WHERE aggregate_id=? AND event_type='graph_committed'`, run.ID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if events != 2 || outbox != 2 {
		t.Fatalf("event/outbox not atomic: %d/%d", events, outbox)
	}
	recovered, err := graphrun.NewSubGraphStore(db).Transact(ctx, "default", run.ID, "repair", nil)
	if err != nil || recovered.Revision != 2 {
		t.Fatalf("recovery: %v %#v", err, recovered)
	}
}
func TestGraphDecisionReplayAndRollback(t *testing.T) {
	db, run, store := durableFixture(t)
	ctx := context.Background()
	patch := entities.GraphPatch{ID: "accepted", GraphRunID: run.ID, SubGraphInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "append", Node: &entities.GraphNode{ID: "test", Kind: entities.NoopNode}}}}
	first, err := store.Transact(ctx, "default", run.ID, "repair", &patch)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Transact(ctx, "default", run.ID, "repair", &patch)
	if err != nil || again.Revision != first.Revision {
		t.Fatal("committed decision was reapplied", err)
	}
	patch.Operations[0].Node.ID = "different"
	if _, err = store.Transact(ctx, "default", run.ID, "repair", &patch); !errors.Is(err, graphrun.ErrGraphConflict) {
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
	item := &entities.NodeRun{BaseEntity: entities.BaseEntity{ID: "seed-attempt", Metadata: map[string]any{"node": entities.CloneGraphNode(&entities.GraphNode{ID: "seed", Kind: entities.NoopNode})}}, GraphRunID: run.ID, NodeInstanceID: "repair/seed", SubGraphInstanceID: "repair", GraphRevision: 1, DispatchEpoch: 1, FencingToken: 1, Attempt: 1, Status: entities.NodeRunPending}
	if err := noderun.NewNodeRunSQLiteStore(db).Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	_, err := store.Transact(ctx, "default", run.ID, "repair", &entities.GraphPatch{ID: "unsafe", GraphRunID: run.ID, SubGraphInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "replace_pending", NodeInstanceID: "seed", Node: &entities.GraphNode{ID: "seed", Kind: entities.NoopNode}}}})
	if err == nil {
		t.Fatal("dispatched noderun was replaced")
	}
	got, _ := store.Transact(ctx, "default", run.ID, "repair", nil)
	if got.Revision != 1 {
		t.Fatal("deferred patch changed old state")
	}
}
func TestExecutionClaimTerminalAndOutboxRecovery(t *testing.T) {
	db, run, _ := durableFixture(t)
	ctx := context.Background()
	repo := noderun.NewNodeRunSQLiteStore(db)
	item := &entities.NodeRun{BaseEntity: entities.BaseEntity{ID: "attempt-1", Metadata: map[string]any{"node": &entities.GraphNode{ID: "top", Kind: entities.NoopNode}}}, GraphRunID: run.ID, NodeInstanceID: "top", DispatchEpoch: 1, FencingToken: 1, Attempt: 1, Status: entities.NodeRunPending, ExecutionID: "plan-1-attempt-1"}
	if err := repo.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	plan := &entities.ExecutionPlan{PlanID: "plan-1", NodeRunID: item.ID, NodeInstanceID: "top", DispatchEpoch: 1, GraphRunID: run.ID, AgentGraphName: "parent", Namespace: "default", RuntimeClusterID: "cluster", NodeKind: entities.NoopNode, Node: &entities.GraphNode{ID: "top", Kind: entities.NoopNode}}
	if err := noderun.DispatchExecution(ctx, db, plan); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		claimed, err := noderun.ClaimExecution(ctx, db, run.ID, item.ID, "top", "tm/slot", 1)
		if err != nil || claimed != (i == 0) {
			t.Fatalf("claim %d: %v %v", i, claimed, err)
		}
	}
	item.Status = entities.NodeRunSucceeded
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
	patch := entities.GraphPatch{ID: "new-dependency", GraphRunID: run.ID, SubGraphInstanceID: "repair", ExpectedRevision: 1, Operations: []entities.PatchOperation{{Kind: "append", Node: &entities.GraphNode{ID: "prerequisite", Kind: entities.NoopNode}}, {Kind: "update_pending_edges", From: "prerequisite", To: "seed"}}}
	if _, err := store.Transact(ctx, "default", run.ID, "repair", &patch); err != nil {
		t.Fatal(err)
	}
	old := &entities.NodeRun{BaseEntity: entities.BaseEntity{ID: "stale-ready", Metadata: map[string]any{"node": entities.CloneGraphNode(&entities.GraphNode{ID: "seed", Kind: entities.NoopNode})}}, GraphRunID: run.ID, NodeInstanceID: "repair/seed", SubGraphInstanceID: "repair", GraphRevision: 1, DispatchEpoch: 1, FencingToken: 1, Attempt: 1, Status: entities.NodeRunPending}
	if err := noderun.NewNodeRunSQLiteStore(db).Save(ctx, old); err == nil {
		t.Fatal("stale Ready snapshot dispatched across a committed graph revision")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orh_node_run WHERE id=?`, old.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale admission wrote an execution fact", err)
	}
}
