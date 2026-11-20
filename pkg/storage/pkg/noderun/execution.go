package noderun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"

	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
)

var ErrExecutionConflict = errors.New("execution state conflict")

func executionConflict(message string) error {
	return fmt.Errorf("%w: %s", ErrExecutionConflict, message)
}

// lockRun serializes noderun admission with graph changes. Existing Attempt rows
// are immutable terminal facts; repeated completion is an idempotent no-op.
func prepareExecution(ctx context.Context, tx *storage.SQLTransaction, item *entities.NodeRun) (bool, error) {
	var metadata []byte
	var status string
	if err := tx.QueryRow(ctx, `SELECT metadata,status FROM orh_graph_run WHERE id=?`+tx.LockSuffix(), item.GraphRunID).Scan(&metadata, &status); err != nil {
		return false, err
	}
	if status == string(entities.GraphRunCancelled) || status == "DELETED" {
		return false, executionConflict("run is cancelled")
	}
	var newest int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt),0) FROM orh_node_run WHERE graph_run_id=? AND node_instance_id=?`, item.GraphRunID, item.NodeInstanceID).Scan(&newest); err != nil {
		return false, err
	}
	if newest > item.Attempt {
		return false, executionConflict("obsolete noderun attempt")
	}
	var oldStatus entities.NodeRunStatus
	var oldFence int64
	var oldOutput, oldError []byte
	err := tx.QueryRow(ctx, `SELECT status,fencing_token,output,error FROM orh_node_run WHERE id=?`, item.ID).Scan(&oldStatus, &oldFence, &oldOutput, &oldError)
	if err == nil {
		if item.FencingToken < oldFence {
			return false, executionConflict("stale noderun fence")
		}
		if oldStatus == entities.NodeRunSucceeded || oldStatus == entities.NodeRunFailed || oldStatus == entities.NodeRunSkipped {
			var output any
			_ = json.Unmarshal(oldOutput, &output)
			a, _ := json.Marshal(output)
			b, _ := json.Marshal(item.Output)
			var failure map[string]any
			_ = json.Unmarshal(oldError, &failure)
			message, _ := failure["message"].(string)
			// A nil result and an empty map both represent no output in legacy plans.
			equivalent := string(a) == string(b) || (len(item.Output) == 0 && (output == nil || string(a) == "{}"))
			if oldStatus != item.Status || !equivalent || message != item.Error {
				return false, executionConflict("terminal attempt is immutable")
			}
			return true, nil
		}
	} else if !storage.IsNoRows(err) {
		return false, err
	}
	if oldStatus == "" {
		var latest int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt),0) FROM orh_node_run WHERE graph_run_id=? AND node_instance_id=?`, item.GraphRunID, item.NodeInstanceID).Scan(&latest); err != nil {
			return false, err
		}
		if latest >= item.Attempt {
			return false, executionConflict("obsolete noderun attempt")
		}
		// Only new admissions are checked against the current graph. A result from
		// an unaffected older revision remains valid for its immutable Attempt.
		if item.SubGraphInstanceID != "" && !strings.HasPrefix(item.NodeInstanceID, item.SubGraphInstanceID+"/__supervisor_") {
			var meta struct {
				SubGraphs map[string]*entities.SubGraph `json:"subgraphs"`
			}
			if err := json.Unmarshal(metadata, &meta); err != nil {
				return false, err
			}
			prefix := item.SubGraphInstanceID + "/"
			if !strings.HasPrefix(item.NodeInstanceID, prefix) {
				return false, executionConflict("invalid subgraph node identity")
			}
			nodeID := strings.TrimPrefix(item.NodeInstanceID, prefix)
			state := meta.SubGraphs[item.SubGraphInstanceID]
			if state == nil || state.Sealed {
				return false, executionConflict("subgraph is absent or sealed")
			}
			if state.Revision != item.GraphRevision {
				return false, executionConflict("stale graph dispatch revision")
			}
			var spec *entities.GraphNode
			data, _ := json.Marshal(item.Metadata["node"])
			_ = json.Unmarshal(data, &spec)
			found := false
			for _, node := range state.Graph.Nodes {
				if node.ID == nodeID {
					found = true
					expected, _ := json.Marshal(entities.CloneGraphNode(&node))
					actual, _ := json.Marshal(spec)
					if string(expected) != string(actual) {
						return false, executionConflict("stale node dispatch specification")
					}
					break
				}
			}
			if !found {
				return false, executionConflict("node does not belong to subgraph")
			}
		}
	}
	return false, nil
}

func appendResultEvent(ctx context.Context, tx *storage.SQLTransaction, item *entities.NodeRun) error {
	if err := appendNodeState(ctx, tx, item); err != nil {
		return err
	}

	if item.Status != entities.NodeRunSucceeded && item.Status != entities.NodeRunFailed && item.Status != entities.NodeRunSkipped {
		return nil
	}
	var graphName string
	if err := tx.QueryRow(ctx, `SELECT agent_graph_id FROM orh_graph_run WHERE id=?`, item.GraphRunID).Scan(&graphName); err != nil {
		return err
	}
	planID := strings.TrimSuffix(item.ExecutionID, fmt.Sprintf("-attempt-%d", item.Attempt))
	id := storage.StableEventID(item.ID, "result", fmt.Sprint(item.DispatchEpoch))
	payload, e := json.Marshal(map[string]any{"message_id": id, "plan_id": planID, "graph_run_id": item.GraphRunID, "node_instance_id": item.NodeInstanceID, "node_run_id": item.ID, "dispatch_epoch": item.DispatchEpoch, "state": item.Status, "output": item.Output, "error": item.Error})
	if e != nil {
		return e
	}
	topic := fmt.Sprintf("flowgent/v1/%s/agent-graphs/%s/graph-runs/%s/exec/results", item.Namespace, graphName, item.GraphRunID)
	return storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: id, Namespace: item.Namespace, AggregateID: item.ID, AggregateSeq: 3, Type: "node_result", Topic: topic, Payload: payload})
}

// ClaimExecution is a persistent inbox admission: duplicate MQTT deliveries
// cannot start the same Attempt twice. An unresolved running attempt is never
// stolen automatically, since its external effect may already have happened.
func ClaimExecution(ctx context.Context, db any, runID, id, instance, owner string, epoch int64) (bool, error) {
	claimed := false
	err := storage.WithSQLTransaction(ctx, db, func(tx *storage.SQLTransaction) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM orh_graph_run WHERE id=?`+tx.LockSuffix(), runID).Scan(&status); err != nil {
			return err
		}
		if status == "CANCELLED" || status == "DELETED" {
			return executionConflict("run is cancelled")
		}
		var metadata []byte
		var nodeKey string
		var fence int64
		if err := tx.QueryRow(ctx, `SELECT metadata,node_instance_id,fencing_token FROM orh_node_run WHERE id=? AND graph_run_id=?`, id, runID).Scan(&metadata, &nodeKey, &fence); err != nil {
			return err
		}
		var meta struct {
			NodeInstanceID string `json:"node_instance_id"`
			DispatchEpoch  int64  `json:"dispatch_epoch"`
		}
		if err := json.Unmarshal(metadata, &meta); err != nil {
			return err
		}
		if meta.NodeInstanceID != instance || meta.DispatchEpoch != epoch || fence != epoch {
			return executionConflict("dispatch identity conflict")
		}
		n, err := tx.Exec(ctx, `UPDATE orh_node_run SET status='RUNNING',lease_owner=?,lease_expires_at=? WHERE id=? AND graph_run_id=? AND status='PENDING' AND fencing_token=?`, owner, time.Now().UTC().Add(2*time.Minute), id, runID, epoch)
		claimed = n == 1
		if err == nil && claimed {
			_, err = tx.Exec(ctx, `UPDATE orh_dispatch SET status='claimed',lease_owner=?,lease_expires_at=? WHERE node_run_id=?`, owner, time.Now().UTC().Add(2*time.Minute), id)
		}
		return err
	})
	return claimed, err
}

// DispatchExecution commits the immutable DTO and its MQTT outbox atomically.
// This is an internal action on a NodeRun, not a new public plan resource.
func DispatchExecution(ctx context.Context, db any, plan *entities.ExecutionPlan) error {
	return storage.WithSQLTransaction(ctx, db, func(tx *storage.SQLTransaction) error {
		var runStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM orh_graph_run WHERE id=? AND namespace_id=?`+tx.LockSuffix(), plan.GraphRunID, plan.Namespace).Scan(&runStatus); err != nil {
			return err
		}
		if runStatus == "CANCELLED" || runStatus == "DELETED" {
			return executionConflict("run is cancelled")
		}
		var nodeKey, status string
		var metadata []byte
		var fence int64
		if err := tx.QueryRow(ctx, `SELECT node_instance_id,status,fencing_token,metadata FROM orh_node_run WHERE id=? AND graph_run_id=?`, plan.NodeRunID, plan.GraphRunID).Scan(&nodeKey, &status, &fence, &metadata); err != nil {
			return err
		}
		if nodeKey != plan.NodeInstanceID || fence != plan.DispatchEpoch {
			return executionConflict("dispatch identity conflict")
		}
		var meta struct {
			Spec *entities.GraphNode `json:"node"`
		}
		if err := json.Unmarshal(metadata, &meta); err != nil {
			return err
		}
		expected, _ := json.Marshal(meta.Spec)
		actual, _ := json.Marshal(plan.Node)
		if meta.Spec == nil || string(expected) != string(actual) || string(meta.Spec.Kind) != string(plan.NodeKind) {
			return executionConflict("dispatch specification differs from admitted node")
		}
		if status != "PENDING" {
			return nil
		}
		data, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		id := storage.StableEventID(plan.NodeRunID, "dispatch", fmt.Sprint(plan.DispatchEpoch))
		if _, err = tx.Exec(ctx, `INSERT INTO orh_dispatch(id,namespace_id,node_run_id,payload,fencing_token,idempotency_key) VALUES(?,?,?,?,?,?) ON CONFLICT(node_run_id) DO NOTHING`, id, plan.Namespace, plan.NodeRunID, string(data), plan.DispatchEpoch, id); err != nil {
			return err
		}
		var committed []byte
		if err = tx.QueryRow(ctx, `SELECT payload FROM orh_dispatch WHERE node_run_id=?`, plan.NodeRunID).Scan(&committed); err != nil {
			return err
		}
		// Trace propagation may differ after a restart; replay the committed DTO.
		topic := fmt.Sprintf("flowgent/v1/%s/clusters/%s/agent-graphs/%s/graph-runs/%s/exec/plans", plan.Namespace, plan.RuntimeClusterID, plan.AgentGraphID, plan.GraphRunID)
		return storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: id, Namespace: plan.Namespace, AggregateID: plan.NodeRunID, AggregateSeq: 1, Type: "node_dispatch", Topic: topic, Payload: committed})
	})
}

// appendNodeState is the incremental state log between GraphCheckpoints. The
// state, immutable event, graph progress and result Outbox share one commit.
func appendNodeState(ctx context.Context, tx *storage.SQLTransaction, node *entities.NodeRun) error {
	state, err := json.Marshal(struct {
		Status     entities.NodeRunStatus
		Checkpoint map[string]any
		Output     map[string]any
		Error      string
		Snapshot   *entities.WorkspaceSnapshot
		Epoch      int64
	}{node.Status, node.Checkpoint, node.Output, node.Error, node.WorkspaceSnapshot, node.DispatchEpoch})
	if err != nil {
		return err
	}
	id := storage.StableEventID(node.ID, string(state))
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orh_event WHERE id=?)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var sequence int64
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM orh_event WHERE graph_run_id=?`, node.GraphRunID).Scan(&sequence); err != nil {
		return err
	}
	payload, err := json.Marshal(node)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO orh_event(id,namespace_id,graph_run_id,node_run_id,sequence,type,payload,idempotency_key) VALUES(?,?,?,?,?,'node_state_committed',?,?)`, id, node.Namespace, node.GraphRunID, node.ID, sequence, string(payload), id); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT metadata FROM orh_graph_run WHERE id=?`, node.GraphRunID).Scan(&raw); err != nil {
		return err
	}
	metadata := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err = json.Unmarshal(raw, &metadata); err != nil {
			return err
		}
	}
	progress, _ := metadata["execution_progress"].(map[string]any)
	if progress == nil {
		progress = map[string]any{}
	}
	progress["last_event_seq"] = sequence
	metadata["execution_progress"] = progress
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE orh_graph_run SET metadata=? WHERE id=?`, string(data), node.GraphRunID)
	return err
}

func appendMessages(ctx context.Context, tx *storage.SQLTransaction, node *entities.NodeRun, checkpointID string) error {
	data, err := json.Marshal(node.Checkpoint)
	if err != nil {
		return err
	}
	var checkpoint entities.NodeCheckpoint
	if err = json.Unmarshal(data, &checkpoint); err != nil {
		return err
	}
	for ordinal, message := range checkpoint.Messages {
		sum := sha256.Sum256([]byte(message.Content))
		hash := hex.EncodeToString(sum[:])
		var role, existing string
		err := tx.QueryRow(ctx, `SELECT role,content_hash FROM orh_node_message WHERE node_run_id=? AND ordinal=?`, node.ID, ordinal).Scan(&role, &existing)
		if err == nil {
			if role != message.Role || existing != hash {
				return executionConflict("committed context message is immutable")
			}
			continue
		}
		if !storage.IsNoRows(err) {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO orh_node_message(id,namespace_id,node_run_id,checkpoint_id,ordinal,role,content,content_hash) VALUES(?,?,?,?,?,?,?,?)`, uuid.NewString(), node.Namespace, node.ID, checkpointID, ordinal, message.Role, message.Content, hash); err != nil {
			return err
		}
	}
	return nil
}

// DispatchSandbox persists the physical Sandbox handoff in the same event /
// Outbox protocol as JM→TM dispatch. Credentials are resolved at the worker.
func DispatchSandbox(ctx context.Context, db any, trigger *model.SandboxTrigger) error {
	return storage.WithSQLTransaction(ctx, db, func(tx *storage.SQLTransaction) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM orh_graph_run WHERE id=? AND namespace_id=?`+tx.LockSuffix(), trigger.GraphRunID, trigger.Namespace).Scan(&status); err != nil {
			return err
		}
		if status == "CANCELLED" || status == "DELETED" {
			return executionConflict("graph run is cancelled")
		}
		var epoch int64
		var nodeStatus string
		if err := tx.QueryRow(ctx, `SELECT status,fencing_token FROM orh_node_run WHERE id=? AND graph_run_id=?`, trigger.NodeRunID, trigger.GraphRunID).Scan(&nodeStatus, &epoch); err != nil {
			return err
		}
		if epoch != trigger.DispatchEpoch || nodeStatus != "RUNNING" {
			return executionConflict("sandbox execution identity conflict")
		}
		trigger.Env = nil
		data, err := json.Marshal(trigger)
		if err != nil {
			return err
		}
		id := storage.StableEventID(trigger.NodeRunID, "sandbox", fmt.Sprint(trigger.DispatchEpoch))
		topic := fmt.Sprintf("flowgent/v1/%s/clusters/%s/agent-graphs/%s/graph-runs/%s/sandbox/trigger", trigger.Namespace, trigger.RuntimeClusterID, trigger.AgentGraphID, trigger.GraphRunID)
		return storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: id, Namespace: trigger.Namespace, AggregateID: trigger.NodeRunID, AggregateSeq: 2, Type: "sandbox_dispatch", Topic: topic, Payload: data})
	})
}
