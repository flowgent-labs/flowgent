package graphrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
)

var ErrGraphConflict = errors.New("graph revision conflict")

// SubGraphStore extends the existing run repository without a second runtime
// table. Each accepted graph is retained in orh_event; metadata is its current
// materialized view. All writes serialize with NodeRun admission on orh_graph_run.
type SubGraphStore struct{ db any }

func NewSubGraphStore(db any) *SubGraphStore { return &SubGraphStore{db: db} }
func (s *SubGraphStore) Transact(ctx context.Context, namespace, runID, instance string, patch *entities.GraphPatch) (*entities.SubGraph, error) {
	var result *entities.SubGraph
	err := storage.WithSQLTransaction(ctx, s.db, func(tx *storage.SQLTransaction) error {
		scope := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run")
		if scope.Where == "0=1" {
			return storage.ErrFlowgentSqlScopeDenied
		}
		// Keep scope evaluation in SQL, including AuthGuard resource restrictions.
		where, args := scope.SQLiteWhere()
		args = append([]any{runID, namespace}, args...)
		var metadata, definition []byte
		var rowVersion int64
		var runStatus string
		q := `SELECT metadata,row_version,status FROM orh_graph_run WHERE id=? AND namespace_id=? AND (` + where + `)` + tx.LockSuffix()
		if e := tx.QueryRow(ctx, q, args...).Scan(&metadata, &rowVersion, &runStatus); e != nil {
			return e
		}
		if runStatus != "PENDING" && runStatus != "RUNNING" && runStatus != "PAUSED" {
			return fmt.Errorf("run is terminal")
		}
		meta := map[string]json.RawMessage{}
		if len(metadata) > 0 {
			if e := json.Unmarshal(metadata, &meta); e != nil {
				return e
			}
		}
		if meta == nil {
			meta = map[string]json.RawMessage{}
		}
		states := map[string]*entities.SubGraph{}
		if len(meta["subgraphs"]) > 0 {
			if e := json.Unmarshal(meta["subgraphs"], &states); e != nil {
				return e
			}
		}
		current := states[instance]
		if current == nil {
			if patch != nil {
				return fmt.Errorf("subgraph not initialized")
			}
			if e := tx.QueryRow(ctx, `SELECT snapshot FROM orh_graph_run WHERE id=? AND namespace_id=?`, runID, namespace).Scan(&definition); e != nil {
				return e
			}
			var graph entities.AgentGraph
			if e := json.Unmarshal(definition, &graph); e != nil {
				return e
			}
			var config entities.SubGraphConfig
			parts := strings.Split(instance, "/")
			if len(parts) > 16 {
				return fmt.Errorf("subgraph depth exceeded")
			}
			for depth, part := range parts {
				found := false
				for _, n := range graph.Nodes {
					if n.ID != part {
						continue
					}
					found = true
					if n.SubGraph == nil {
						return fmt.Errorf("subgraph configuration is required")
					}
					config = *n.SubGraph
					if n.Kind != entities.SubGraphNode {
						return fmt.Errorf("node is not a subgraph")
					}
					if e := config.Validate(); e != nil {
						return e
					}
					if config.IndependentRun {
						return fmt.Errorf("independent subgraph runs are not supported")
					}
					if config.Limits.MaxDepth > 0 && depth+1 > config.Limits.MaxDepth {
						return fmt.Errorf("subgraph depth budget exhausted")
					}
					if pinned := states[strings.Join(parts[:depth+1], "/")]; pinned != nil {
						graph = pinned.Graph
						break
					}
					if config.DefinitionSnapshot == nil {
						return fmt.Errorf("subgraph definition is not pinned")
					}
					graph = *config.DefinitionSnapshot
					break
				}
				if !found {
					return fmt.Errorf("subgraph instance does not belong to run definition")
				}
			}
			if config.Adaptive() {
				if config.PolicyRef != "" || config.Contract.InputSchemaRef != "" || config.Contract.OutputSchemaRef != "" || config.Contract.CompletionPolicyRef != "" {
					return fmt.Errorf("named subgraph policies and schema references require a resolver; use inline graph schemas and required_nodes")
				}
				if config.Limits.MaxCreatedNodes == 0 {
					config.Limits.MaxCreatedNodes = 128
				}
				if config.Limits.MaxParallelism == 0 {
					config.Limits.MaxParallelism = 4
				}
				if config.Limits.MaxPatches == 0 {
					config.Limits.MaxPatches = 12
				}
				if config.Limits.MaxDepth == 0 {
					config.Limits.MaxDepth = 4
				}
				if config.Limits.MaxActiveDuration == "" {
					config.Limits.MaxActiveDuration = "30m"
				}

				if e := graph.ValidateAcyclic(); e != nil {
					return e
				}
			}
			current = &entities.SubGraph{InstanceID: instance, Revision: 1, Config: config, Graph: graph, Templates: graph.Nodes, CreatedNodes: len(graph.Nodes), Decisions: map[string]string{}}
			if config.Limits.MaxCreatedNodes > 0 && current.CreatedNodes > config.Limits.MaxCreatedNodes {
				return fmt.Errorf("initial node budget exceeded")
			}
		} else if patch == nil {
			result = current
			return nil
		}
		eventID := storage.StableEventID(runID, instance, "init")
		if patch != nil {
			if patch.GraphRunID != runID || patch.SubGraphInstanceID != instance {
				return fmt.Errorf("patch scope mismatch")
			}
			payload, e := json.Marshal(patch)
			if e != nil {
				return e
			}
			hash := storage.StableEventID(string(payload))
			if old, ok := current.Decisions[patch.ID]; ok {
				if old != hash {
					return ErrGraphConflict
				}
				result = current
				return nil
			}
			if current.Revision != patch.ExpectedRevision {
				return ErrGraphConflict
			}
			statuses := map[string]entities.NodeRunStatus{}
			for _, node := range current.Graph.Nodes {
				var status entities.NodeRunStatus
				e := tx.QueryRow(ctx, `SELECT status FROM orh_node_run WHERE graph_run_id=? AND node_instance_id=? ORDER BY attempt DESC LIMIT 1`, runID, instance+"/"+node.ID).Scan(&status)
				if e != nil && !errors.Is(e, sql.ErrNoRows) && e.Error() != "no rows in result set" {
					return e
				}
				// A persisted PENDING row already has an immutable dispatch plan.
				if status == entities.NodeRunPending {
					status = entities.NodeRunRunning
				}
				statuses[node.ID] = status
			}
			next, e := current.Apply(*patch, statuses)
			if e != nil {
				return e
			}
			if next.Decisions == nil {
				next.Decisions = map[string]string{}
			}
			next.Decisions[patch.ID] = hash
			current = next
			eventID = storage.StableEventID(runID, instance, patch.ID)
		}
		var sequence int64
		if e := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM orh_event WHERE graph_run_id=?`, runID).Scan(&sequence); e != nil {
			return e
		}
		var progress struct {
			LastEventSeq int64 `json:"last_event_seq"`
		}
		if len(meta["execution_progress"]) > 0 {
			if e := json.Unmarshal(meta["execution_progress"], &progress); e != nil {
				return e
			}
		}
		progress.LastEventSeq = sequence
		meta["execution_progress"], _ = json.Marshal(progress)
		for _, node := range current.Graph.Nodes {
			if strings.Contains(node.ID, "/") || len(instance+"/"+node.ID) > 128 {
				return fmt.Errorf("subgraph node identity exceeds scoped storage boundary")
			}
		}
		states[instance] = current
		meta["subgraphs"], _ = json.Marshal(states)
		encoded, e := json.Marshal(meta)
		if e != nil {
			return e
		}
		count, e := tx.Exec(ctx, `UPDATE orh_graph_run SET metadata=? WHERE id=? AND namespace_id=? AND row_version=?`, string(encoded), runID, namespace, rowVersion)
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrGraphConflict
		}
		data, e := json.Marshal(current)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO orh_event(id,namespace_id,graph_run_id,sequence,type,payload,idempotency_key) VALUES(?,?,?,?, 'graph_committed',?,?)`, eventID, namespace, runID, sequence, string(data), eventID); e != nil {
			return e
		}
		topic := fmt.Sprintf("flowgent/v1/%s/graph-runs/%s/graph/committed", namespace, runID)
		notice, _ := json.Marshal(map[string]any{"message_id": eventID, "graph_run_id": runID, "subgraph_instance_id": instance, "graph_revision": current.Revision, "event_seq": sequence, "committed_at": time.Now().UTC()})
		if e = storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: eventID, Namespace: namespace, AggregateID: runID, AggregateSeq: sequence, Type: "graph_committed", Topic: topic, Payload: notice}); e != nil {
			return e
		}
		result = current
		return nil
	})
	return result, err
}
