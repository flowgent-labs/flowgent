package graphrun

import (
	"context"
	"encoding/json"
	"fmt"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type runRecord struct {
	Snapshot             entities.AgentGraph     `db:"snapshot"`
	AgentGraphID         string                  `db:"agent_graph_id"`
	AgentGraphName       string                  `db:"agent_graph_name"`
	AgentGraphRevisionID string                  `db:"agent_graph_revision_id"`
	Status               entities.GraphRunStatus `db:"status"`
	Input                map[string]any          `db:"input"`
	Output               map[string]any          `db:"output"`
	Error                map[string]any          `db:"error"`
	RunInstruction       string                  `db:"run_instruction"`
	SummarizeEnabled     bool                    `db:"summarize_enabled"`
	ContextSnapshot      map[string]any          `db:"context_snapshot"`
	TriggerType          string                  `db:"trigger_type"`
	TriggerSource        string                  `db:"trigger_source"`
	TriggerPayload       map[string]any          `db:"trigger_payload"`
	RuntimeMode          entities.RuntimeMode    `db:"runtime_mode"`
	RuntimeClusterID     string                  `db:"runtime_cluster_id"`
	StartedAt            *time.Time              `db:"started_at"`
	FinishedAt           *time.Time              `db:"finished_at"`
	ID                   string                  `db:"id"`
	Description          string                  `db:"description"`
	Namespace            string                  `db:"namespace_id"`
	CreatedAt            time.Time               `db:"created_at"`
	CreatedBy            string                  `db:"created_by"`
	UpdatedAt            time.Time               `db:"updated_at"`
	UpdatedBy            string                  `db:"updated_by"`
	RowVersion           int64                   `db:"row_version"`
	Metadata             map[string]any          `db:"metadata"`
}

func (r *runRecord) entity() *entities.GraphRun {
	item := &entities.GraphRun{Snapshot: r.Snapshot, BaseEntity: entities.BaseEntity{ID: r.ID, Description: r.Description, Namespace: r.Namespace, Status: string(r.Status), CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy, RowVersion: r.RowVersion, Metadata: r.Metadata}, AgentGraphID: r.AgentGraphID, AgentGraphName: r.AgentGraphName, AgentGraphRevisionID: r.AgentGraphRevisionID, Status: r.Status, Input: r.Input, Output: r.Output, RunInstruction: r.RunInstruction, SummarizeEnabled: r.SummarizeEnabled, ContextSnapshot: r.ContextSnapshot, TriggerType: r.TriggerType, TriggerSource: r.TriggerSource, TriggerPayload: r.TriggerPayload, RuntimeMode: r.RuntimeMode, RuntimeClusterID: r.RuntimeClusterID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}
	if raw, ok := r.Metadata["subgraphs"]; ok {
		data, _ := json.Marshal(raw)
		_ = json.Unmarshal(data, &item.SubGraphs)
	}
	// Internal materialized state is projected once through typed domain fields.
	if item.Metadata != nil {
		clean := map[string]any{}
		for key, value := range item.Metadata {
			if key != "subgraphs" && key != "execution_progress" {
				clean[key] = value
			}
		}
		item.Metadata = clean
	}
	item.StateVersion = r.RowVersion
	if progress, ok := r.Metadata["execution_progress"].(map[string]any); ok {
		if v, ok := progress["last_event_seq"].(float64); ok {
			item.LastEventSeq = int64(v)
		}
	}
	if message, ok := r.Error["message"].(string); ok {
		item.Error = message
	}
	if revision, ok := r.ContextSnapshot["agent_graph_revision"].(map[string]any); ok {
		if value, ok := revision["revision"].(float64); ok {
			item.AgentGraphRevision = int64(value)
		}
	}
	return item
}

func runError(value string) []byte {
	if value == "" {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"message": value})
	return data
}
func runJSON(value any) []byte {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return data
}

func collectAgentNames(nodes []entities.GraphNode, result map[string]struct{}) {
	for i := range nodes {
		if nodes[i].SubGraph != nil {
			if nodes[i].SubGraph.Supervisor != "" {
				result[nodes[i].SubGraph.Supervisor] = struct{}{}
			}
			if nodes[i].SubGraph.DefinitionSnapshot != nil {
				collectAgentNames(nodes[i].SubGraph.DefinitionSnapshot.Nodes, result)
			}
		}
		if nodes[i].Agent != "" {
			result[nodes[i].Agent] = struct{}{}
		}
		if nodes[i].Node != nil {
			collectAgentNames([]entities.GraphNode{*nodes[i].Node}, result)
		}
	}
}

type ListFilter struct {
	Namespace    string
	Status       string
	RuntimeMode  string
	K8sNamespace string
	AgentGraphID string
	Page         entities.PageRequest
}

type MetricRequest struct {
	Namespace string
	Since     time.Time
	Until     time.Time
	Buckets   int
}

// IGraphRunStore is the agentgraph run entity store interface.
type IGraphRunStore interface {
	Get(ctx context.Context, id string) (*entities.GraphRun, error)
	List(ctx context.Context, filter ListFilter) (*entities.Page[entities.GraphRun], error)
	Metrics(ctx context.Context, req MetricRequest) (*entities.GraphRunMetrics, error)
	HasActiveForAgentGraph(ctx context.Context, namespace, graphID string) (bool, error)
	Save(ctx context.Context, entity *entities.GraphRun) error
	Delete(ctx context.Context, id string) error
	Create(ctx context.Context, entity *entities.GraphRun) error
	Update(ctx context.Context, entity *entities.GraphRun) error
	Cancel(ctx context.Context, id string) error
}

type metricRow struct {
	bucket          int
	status          entities.GraphRunStatus
	count           int64
	durationTotalMs int64
	durationCount   int64
}

func buildMetrics(req MetricRequest, rows []metricRow) *entities.GraphRunMetrics {
	if req.Buckets < 1 || !req.Until.After(req.Since) {
		return &entities.GraphRunMetrics{Buckets: []entities.GraphRunMetricBucket{}}
	}
	width := req.Until.Sub(req.Since) / time.Duration(req.Buckets)
	result := &entities.GraphRunMetrics{Buckets: make([]entities.GraphRunMetricBucket, req.Buckets)}
	for i := range result.Buckets {
		start := req.Since.Add(time.Duration(i) * width)
		result.Buckets[i] = entities.GraphRunMetricBucket{StartTime: start, EndTime: start.Add(width)}
	}
	bucketDurationTotals := make([]int64, len(result.Buckets))
	bucketDurationCounts := make([]int64, len(result.Buckets))
	for _, row := range rows {
		if row.bucket < 0 || row.bucket >= len(result.Buckets) {
			continue
		}
		result.Total += row.count
		bucketDurationTotals[row.bucket] += row.durationTotalMs
		bucketDurationCounts[row.bucket] += row.durationCount
		bucket := &result.Buckets[row.bucket]
		switch row.status {
		case entities.GraphRunRunning:
			result.Running += row.count
			bucket.Running += row.count
		case entities.GraphRunCompleted:
			result.Completed += row.count
			bucket.Completed += row.count
		case entities.GraphRunFailed:
			result.Failed += row.count
			bucket.Failed += row.count
		case entities.GraphRunCancelled:
			result.Cancelled += row.count
		}
	}
	var durationTotalMs, durationCount int64
	for index := range result.Buckets {
		durationTotalMs += bucketDurationTotals[index]
		durationCount += bucketDurationCounts[index]
		if bucketDurationCounts[index] > 0 {
			result.Buckets[index].AverageDurationMs = bucketDurationTotals[index] / bucketDurationCounts[index]
		}
	}
	if durationCount > 0 {
		result.AverageDurationMs = durationTotalMs / durationCount
	}
	terminal := result.Completed + result.Failed + result.Cancelled
	if terminal > 0 {
		result.SuccessRate = float64(result.Completed) / float64(terminal)
		result.FailureRate = float64(result.Failed) / float64(terminal)
	}
	return result
}

// pinSubGraphs closes the definition tree in the GraphRun creation transaction.
// Subsequent edits to a referenced definition never retarget an existing run.
func pinSubGraphs(ctx context.Context, tx *storage.SQLTransaction, namespace string, graph *entities.AgentGraph, stack map[string]bool, depth int) error {
	if depth > 16 {
		return fmt.Errorf("subgraph definition depth exceeded")
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		if node.SubGraph != nil {
			ref := node.SubGraph.Ref
			if stack[ref] {
				return fmt.Errorf("recursive subgraph reference: %s", ref)
			}
			var data []byte
			var id string
			var revision int64
			if err := tx.QueryRow(ctx, `SELECT r.id,r.revision,r.definition FROM orh_agent_graph g JOIN orh_agent_graph_revision r ON r.id=g.current_revision_id WHERE g.namespace_id=? AND (g.id=? OR g.name=?) AND g.status<>'DELETED'`, namespace, ref, ref).Scan(&id, &revision, &data); err != nil {
				return err
			}
			var child entities.AgentGraph
			if err := json.Unmarshal(data, &child); err != nil {
				return err
			}
			child.Revision = revision
			stack[ref] = true
			if err := pinSubGraphs(ctx, tx, namespace, &child, stack, depth+1); err != nil {
				return err
			}
			delete(stack, ref)
			node.SubGraph.DefinitionSnapshot = &child
			node.SubGraph.DefinitionRevisionID = id
		}
		if node.Node != nil {
			nested := entities.AgentGraph{Nodes: []entities.GraphNode{*node.Node}}
			if err := pinSubGraphs(ctx, tx, namespace, &nested, stack, depth); err != nil {
				return err
			}
			*node.Node = nested.Nodes[0]
		}
	}
	return nil
}

func appendGraphRunCreated(ctx context.Context, tx *storage.SQLTransaction, run *entities.GraphRun, graphID, graphName string) error {
	id := storage.StableEventID(run.ID, "created")
	data, err := json.Marshal(map[string]any{"message_id": id, "event_type": "CREATED", "graph_run_id": run.ID, "agent_graph_id": graphID, "agent_graph_name": graphName, "namespace_id": run.Namespace, "namespace": run.K8sNamespace, "status": run.Status})
	if err != nil {
		return err
	}
	topic := fmt.Sprintf("flowgent/v1/%s/agent-graphs/%s/graph-runs/%s/ctrl/graph-run/created", run.Namespace, graphID, run.ID)
	return storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: id, Namespace: run.Namespace, AggregateID: run.ID, AggregateSeq: 0, Type: "graph_run_created", Topic: topic, Payload: data})
}
