package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
)

var ErrExecutionConflict = errors.New("execution state conflict")

func executionConflict(message string) error {
	return fmt.Errorf("%w: %s", ErrExecutionConflict, message)
}

// lockRun serializes task admission with graph changes. Existing Attempt rows
// are immutable terminal facts; repeated completion is an idempotent no-op.
func prepareExecution(ctx context.Context, tx *storage.SQLTransaction, item *entities.TaskRunInfo) (bool, error) {
	var metadata []byte
	var status string
	if err := tx.QueryRow(ctx, `SELECT metadata,status FROM orh_run WHERE id=?`+tx.LockSuffix(), item.RunID).Scan(&metadata, &status); err != nil {
		return false, err
	}
	if status == string(entities.RunCancelled) || status == "DELETED" {
		return false, executionConflict("run is cancelled")
	}
	var newest int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt),0) FROM orh_node_run WHERE run_id=? AND node_key=?`, item.RunID, item.NodeKey).Scan(&newest); err != nil {
		return false, err
	}
	if newest > item.Attempt {
		return false, executionConflict("obsolete task attempt")
	}
	var oldStatus entities.TaskStatus
	var oldFence int64
	var oldOutput, oldError []byte
	err := tx.QueryRow(ctx, `SELECT status,fencing_token,output,error FROM orh_node_run WHERE id=?`, item.ID).Scan(&oldStatus, &oldFence, &oldOutput, &oldError)
	if err == nil {
		if item.FencingToken < oldFence {
			return false, executionConflict("stale task fence")
		}
		if oldStatus == entities.Success || oldStatus == entities.Failed || oldStatus == entities.Skipped {
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
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt),0) FROM orh_node_run WHERE run_id=? AND node_key=?`, item.RunID, item.NodeKey).Scan(&latest); err != nil {
			return false, err
		}
		if latest >= item.Attempt {
			return false, executionConflict("obsolete task attempt")
		}
		// Only new admissions are checked against the current graph. A result from
		// an unaffected older revision remains valid for its immutable Attempt.
		if item.SubflowInstanceID != "" && !strings.HasPrefix(item.NodeKey, item.SubflowInstanceID+"/__supervisor_") {
			var meta struct {
				Subflows map[string]*entities.SubflowState `json:"subflows"`
			}
			if err := json.Unmarshal(metadata, &meta); err != nil {
				return false, err
			}
			prefix := item.SubflowInstanceID + "/"
			if !strings.HasPrefix(item.NodeKey, prefix) {
				return false, executionConflict("invalid subflow node identity")
			}
			nodeID := strings.TrimPrefix(item.NodeKey, prefix)
			state := meta.Subflows[item.SubflowInstanceID]
			if state == nil || state.Sealed {
				return false, executionConflict("subflow is absent or sealed")
			}
			if state.Revision != item.GraphRevision {
				return false, executionConflict("stale graph dispatch revision")
			}
			var spec *entities.NodeSpec
			data, _ := json.Marshal(item.Metadata["node_spec"])
			_ = json.Unmarshal(data, &spec)
			found := false
			for _, node := range state.Graph.Nodes {
				if node.ID == nodeID {
					found = true
					expected, _ := json.Marshal(entities.NodeSpecFromNode(&node))
					actual, _ := json.Marshal(spec)
					if string(expected) != string(actual) {
						return false, executionConflict("stale node dispatch specification")
					}
					break
				}
			}
			if !found {
				return false, executionConflict("node does not belong to subflow")
			}
		}
	}
	return false, nil
}

func appendResultEvent(ctx context.Context, tx *storage.SQLTransaction, item *entities.TaskRunInfo) error {
	if item.Status != entities.Success && item.Status != entities.Failed && item.Status != entities.Skipped {
		return nil
	}
	var flowName string
	if err := tx.QueryRow(ctx, `SELECT f.name FROM orh_run r JOIN orh_flow f ON f.id=r.flow_id WHERE r.id=?`, item.RunID).Scan(&flowName); err != nil {
		return err
	}
	planID := strings.TrimSuffix(item.ExecutionID, fmt.Sprintf("-attempt-%d", item.Attempt))
	id := storage.StableEventID(item.ID, "result", fmt.Sprint(item.DispatchEpoch))
	payload, e := json.Marshal(map[string]any{"message_id": id, "plan_id": planID, "agentflow_run_id": item.RunID, "run_id": item.RunID, "node_id": item.NodeKey, "node_instance_id": item.NodeInstanceID, "node_run_id": item.ID, "dispatch_epoch": item.DispatchEpoch, "state": item.Status, "output": item.Output, "error": item.Error})
	if e != nil {
		return e
	}
	topic := fmt.Sprintf("flowgent/v1/%s/flows/%s/runs/%s/exec/results", item.Namespace, flowName, item.RunID)
	return storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: id, Namespace: item.Namespace, AggregateID: item.ID, AggregateSeq: 2, Type: "node_result", Topic: topic, Payload: payload})
}

// ClaimExecution is a persistent inbox admission: duplicate MQTT deliveries
// cannot start the same Attempt twice. An unresolved running attempt is never
// stolen automatically, since its external effect may already have happened.
func ClaimExecution(ctx context.Context, db any, runID, id, instance, owner string, epoch int64) (bool, error) {
	claimed := false
	err := storage.WithSQLTransaction(ctx, db, func(tx *storage.SQLTransaction) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM orh_run WHERE id=?`+tx.LockSuffix(), runID).Scan(&status); err != nil {
			return err
		}
		if status == "CANCELLED" || status == "DELETED" {
			return executionConflict("run is cancelled")
		}
		var metadata []byte
		var nodeKey string
		var fence int64
		if err := tx.QueryRow(ctx, `SELECT metadata,node_key,fencing_token FROM orh_node_run WHERE id=? AND run_id=?`, id, runID).Scan(&metadata, &nodeKey, &fence); err != nil {
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
		n, err := tx.Exec(ctx, `UPDATE orh_node_run SET status='RUNNING',lease_owner=?,lease_expires_at=? WHERE id=? AND run_id=? AND status='PENDING' AND fencing_token=?`, owner, time.Now().UTC().Add(2*time.Minute), id, runID, epoch)
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
		if err := tx.QueryRow(ctx, `SELECT status FROM orh_run WHERE id=? AND namespace_id=?`+tx.LockSuffix(), plan.AgentFlowRunID, plan.Namespace).Scan(&runStatus); err != nil {
			return err
		}
		if runStatus == "CANCELLED" || runStatus == "DELETED" {
			return executionConflict("run is cancelled")
		}
		var nodeKey, status string
		var metadata []byte
		var fence int64
		if err := tx.QueryRow(ctx, `SELECT node_key,status,fencing_token,metadata FROM orh_node_run WHERE id=? AND run_id=?`, plan.TaskID, plan.AgentFlowRunID).Scan(&nodeKey, &status, &fence, &metadata); err != nil {
			return err
		}
		if nodeKey != plan.NodeInstanceID || nodeKey != plan.NodeID || fence != plan.DispatchEpoch {
			return executionConflict("dispatch identity conflict")
		}
		var meta struct {
			Spec *entities.NodeSpec `json:"node_spec"`
		}
		if err := json.Unmarshal(metadata, &meta); err != nil {
			return err
		}
		expected, _ := json.Marshal(meta.Spec)
		actual, _ := json.Marshal(plan.NodeSpec)
		if meta.Spec == nil || string(expected) != string(actual) || string(meta.Spec.Kind) != string(plan.TaskType) {
			return executionConflict("dispatch specification differs from admitted node")
		}
		if status != "PENDING" {
			return nil
		}
		data, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		id := storage.StableEventID(plan.TaskID, "dispatch", fmt.Sprint(plan.DispatchEpoch))
		if _, err = tx.Exec(ctx, `INSERT INTO orh_dispatch(id,namespace_id,node_run_id,payload,fencing_token,idempotency_key) VALUES(?,?,?,?,?,?) ON CONFLICT(node_run_id) DO NOTHING`, id, plan.Namespace, plan.TaskID, string(data), plan.DispatchEpoch, id); err != nil {
			return err
		}
		var committed []byte
		if err = tx.QueryRow(ctx, `SELECT payload FROM orh_dispatch WHERE node_run_id=?`, plan.TaskID).Scan(&committed); err != nil {
			return err
		}
		// Trace propagation may differ after a restart; replay the committed DTO.
		topic := fmt.Sprintf("flowgent/v1/%s/clusters/%s/flows/%s/runs/%s/exec/plans", plan.Namespace, plan.RuntimeClusterID, plan.AgentFlowDefinitionID, plan.AgentFlowRunID)
		return storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: id, Namespace: plan.Namespace, AggregateID: plan.TaskID, AggregateSeq: 1, Type: "node_dispatch", Topic: topic, Payload: committed})
	})
}
