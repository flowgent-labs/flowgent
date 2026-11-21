package graphrun_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/noderun"
)

func checkpointFixture(t *testing.T) (*sql.DB, *entities.GraphRun) {
	t.Helper()
	db := storage.NewSQLiteConn(context.Background(), t.TempDir())
	t.Cleanup(func() { db.Close() })
	graph := &entities.AgentGraph{BaseEntity: entities.BaseEntity{Namespace: "default"}, Name: "checkpoint-graph", Nodes: []entities.GraphNode{{ID: "work", Kind: entities.NoopNode}}}
	if err := agentgraph.NewAgentGraphSQLiteStore(db).CreateSpec(context.Background(), graph, "fixture", "checkpoint"); err != nil {
		t.Fatal(err)
	}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{Namespace: "default"}, AgentGraphName: graph.Name}
	if err := graphrun.NewGraphRunSQLiteStore(db).Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	return db, run
}
func TestGraphCheckpointRestoresCutAndOverlaysCommittedNodes(t *testing.T) {
	db, run := checkpointFixture(t)
	ctx := context.Background()
	nodes := noderun.NewNodeRunSQLiteStore(db)
	store := graphrun.NewCheckpointStore(db)
	node := &entities.NodeRun{BaseEntity: entities.BaseEntity{ID: "node-attempt"}, GraphRunID: run.ID, NodeInstanceID: "work", Attempt: 1, Status: entities.NodeRunPending}
	if err := nodes.Save(ctx, node); err != nil {
		t.Fatal(err)
	}
	first, err := store.Create(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.NodeRuns) != 1 || first.NodeRuns[0].Status != entities.NodeRunPending || first.Snapshot.ID != run.AgentGraphID {
		t.Fatalf("incomplete checkpoint: %+v", first)
	}
	node.Status = entities.NodeRunSucceeded
	node.Output = map[string]any{"receipt": "accepted"}
	if err = nodes.Save(ctx, node); err != nil {
		t.Fatal(err)
	}
	recovery, err := store.Recover(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Checkpoint.ID != first.ID || recovery.NodeRuns[0].Status != entities.NodeRunSucceeded {
		t.Fatal("recovery rewound a committed completion")
	}
	// A later semantically incomplete/corrupt checkpoint must fall back.
	var sequence int64
	db.QueryRow(`SELECT MAX(sequence)+1 FROM orh_event WHERE graph_run_id=?`, run.ID).Scan(&sequence)
	bad, _ := json.Marshal(map[string]any{"id": "broken", "graph_run_id": run.ID, "sha256": "bad"})
	if _, err = db.Exec(`INSERT INTO orh_event(id,namespace_id,graph_run_id,sequence,type,payload,idempotency_key) VALUES('broken','default',?,?,'graph_checkpoint',?,'broken')`, run.ID, sequence, string(bad)); err != nil {
		t.Fatal(err)
	}
	recovery, err = store.Recover(ctx, "default", run.ID)
	if err != nil || recovery.Checkpoint.ID != first.ID || len(recovery.RejectedCheckpoints) != 1 {
		t.Fatalf("fallback failed: %v %+v", err, recovery)
	}
}
func TestGraphRunSnapshotAndCheckpointTransactionsAreImmutable(t *testing.T) {
	db, run := checkpointFixture(t)
	ctx := context.Background()
	store := graphrun.NewCheckpointStore(db)
	if _, err := db.Exec(`UPDATE orh_graph_run SET snapshot='{}' WHERE id=?`, run.ID); err == nil {
		t.Fatal("GraphRun snapshot was mutable")
	}
	first, err := store.Create(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER fail_graph_checkpoint BEFORE INSERT ON orh_event WHEN NEW.type='graph_checkpoint' BEGIN SELECT RAISE(ABORT,'injected checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Create(ctx, "default", run.ID); err == nil {
		t.Fatal("injected failure ignored")
	}
	recovered, err := store.Recover(ctx, "default", run.ID)
	if err != nil || recovered.Checkpoint.ID != first.ID {
		t.Fatal("failed checkpoint replaced completed checkpoint", err)
	}
}
