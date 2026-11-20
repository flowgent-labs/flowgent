package graphrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/noderun"
	"github.com/google/uuid"
)

// CheckpointStore stores consistent snapshots in the existing immutable event
// ledger. Node admission/results and graph patches take the same GraphRun lock.
type CheckpointStore struct{ db any }

func NewCheckpointStore(db any) *CheckpointStore { return &CheckpointStore{db: db} }
func checkpointDigest(checkpoint entities.GraphCheckpoint) (string, error) {
	checkpoint.SHA256 = ""
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func checkpointRun(ctx context.Context, tx *storage.SQLTransaction, namespace, id string) (entities.AgentGraph, map[string]*entities.SubGraph, int64, error) {
	scope := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run")
	where, args := scope.SQLiteWhere()
	args = append([]any{id, namespace}, args...)
	var snapshot entities.AgentGraph
	var graphData, metadata []byte
	var version int64
	err := tx.QueryRow(ctx, `SELECT snapshot,metadata,row_version FROM orh_graph_run WHERE id=? AND namespace_id=? AND status<>'DELETED' AND (`+where+`)`+tx.LockSuffix(), args...).Scan(&graphData, &metadata, &version)
	if err != nil {
		return snapshot, nil, 0, err
	}
	if err = json.Unmarshal(graphData, &snapshot); err != nil {
		return snapshot, nil, 0, err
	}
	var state struct {
		SubGraphs map[string]*entities.SubGraph `json:"subgraphs"`
	}
	if len(metadata) > 0 && string(metadata) != "null" {
		if err = json.Unmarshal(metadata, &state); err != nil {
			return snapshot, nil, 0, err
		}
	}
	return snapshot, state.SubGraphs, version, nil
}
func (s *CheckpointStore) Create(ctx context.Context, namespace, graphRunID string) (*entities.GraphCheckpoint, error) {
	var checkpoint *entities.GraphCheckpoint
	err := storage.WithSQLTransaction(ctx, s.db, func(tx *storage.SQLTransaction) error {
		snapshot, subgraphs, version, err := checkpointRun(ctx, tx, namespace, graphRunID)
		if err != nil {
			return err
		}
		nodes, err := noderun.ReadCheckpointNodes(ctx, tx, graphRunID)
		if err != nil {
			return err
		}
		var sequence int64
		if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM orh_event WHERE graph_run_id=?`, graphRunID).Scan(&sequence); err != nil {
			return err
		}
		checkpoint = &entities.GraphCheckpoint{ID: uuid.NewString(), GraphRunID: graphRunID, EventSeq: sequence, StateVersion: version + 1, Snapshot: snapshot, SubGraphs: subgraphs, NodeRuns: nodes, CreatedAt: time.Now().UTC()}
		checkpoint.SHA256, err = checkpointDigest(*checkpoint)
		if err != nil {
			return err
		}
		data, err := json.Marshal(checkpoint)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO orh_event(id,namespace_id,graph_run_id,sequence,type,payload,idempotency_key) VALUES(?,?,?,?,'graph_checkpoint',?,?)`, checkpoint.ID, namespace, graphRunID, sequence, string(data), "checkpoint:"+checkpoint.ID)
		if err != nil {
			return err
		}
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT metadata FROM orh_graph_run WHERE id=?`, graphRunID).Scan(&raw); err != nil {
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
		progress["checkpoint_id"] = checkpoint.ID
		metadata["execution_progress"] = progress
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE orh_graph_run SET metadata=? WHERE id=?`, string(encoded), graphRunID)
		return err
	})
	return checkpoint, err
}

// Recovery reads current committed NodeRuns after the checkpoint. Completed
// work is never replaced with an older checkpoint's state or executed again.
func (s *CheckpointStore) Recover(ctx context.Context, namespace, graphRunID string) (*entities.GraphRecovery, error) {
	recovery := &entities.GraphRecovery{}
	err := storage.WithSQLTransaction(ctx, s.db, func(tx *storage.SQLTransaction) error {
		snapshot, subgraphs, _, err := checkpointRun(ctx, tx, namespace, graphRunID)
		if err != nil {
			return err
		}
		recovery.Snapshot = snapshot
		recovery.SubGraphs = subgraphs
		recovery.NodeRuns, err = noderun.ReadCheckpointNodes(ctx, tx, graphRunID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,payload FROM orh_event WHERE graph_run_id=? AND namespace_id=? AND type='graph_checkpoint' ORDER BY sequence DESC`, graphRunID, namespace)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var data []byte
			if err = rows.Scan(&id, &data); err != nil {
				return err
			}
			var checkpoint entities.GraphCheckpoint
			decodeErr := json.Unmarshal(data, &checkpoint)
			digest, digestErr := checkpointDigest(checkpoint)
			if decodeErr != nil || digestErr != nil || checkpoint.ID != id || checkpoint.GraphRunID != graphRunID || checkpoint.SHA256 != digest {
				recovery.RejectedCheckpoints = append(recovery.RejectedCheckpoints, id)
				continue
			}
			if checkpoint.Snapshot.ID != snapshot.ID {
				recovery.RejectedCheckpoints = append(recovery.RejectedCheckpoints, id)
				continue
			}
			recovery.Checkpoint = &checkpoint
			break
		}
		return rows.Err()
	})
	return recovery, err
}
