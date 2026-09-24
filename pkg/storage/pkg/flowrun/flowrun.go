package flowrun

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type runRecord struct {
	FlowID           string               `db:"flow_id"`
	FlowName         string               `db:"flow_name"`
	FlowRevisionID   string               `db:"flow_revision_id"`
	Status           entities.RunStatus   `db:"status"`
	Input            map[string]any       `db:"input"`
	Output           map[string]any       `db:"output"`
	Error            map[string]any       `db:"error"`
	RunInstruction   string               `db:"run_instruction"`
	SummarizeEnabled bool                 `db:"summarize_enabled"`
	ContextSnapshot  map[string]any       `db:"context_snapshot"`
	TriggerType      string               `db:"trigger_type"`
	TriggerSource    string               `db:"trigger_source"`
	TriggerPayload   map[string]any       `db:"trigger_payload"`
	RuntimeMode      entities.RuntimeMode `db:"runtime_mode"`
	RuntimeClusterID string               `db:"runtime_cluster_id"`
	StartedAt        *time.Time           `db:"started_at"`
	FinishedAt       *time.Time           `db:"finished_at"`
	ID               string               `db:"id"`
	Description      string               `db:"description"`
	Namespace        string               `db:"namespace_id"`
	CreatedAt        time.Time            `db:"created_at"`
	CreatedBy        string               `db:"created_by"`
	UpdatedAt        time.Time            `db:"updated_at"`
	UpdatedBy        string               `db:"updated_by"`
	RowVersion       int64                `db:"row_version"`
	Metadata         map[string]any       `db:"metadata"`
}

func (r *runRecord) entity() *entities.FlowRunInfo {
	item := &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: r.ID, Description: r.Description, Namespace: r.Namespace, Status: string(r.Status), CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy, RowVersion: r.RowVersion, Metadata: r.Metadata}, FlowID: r.FlowID, FlowName: r.FlowName, AgentFlowID: r.FlowName, FlowRevisionID: r.FlowRevisionID, Status: r.Status, Input: r.Input, Vars: r.Input, Output: r.Output, RunInstruction: r.RunInstruction, SummarizeEnabled: r.SummarizeEnabled, ContextSnapshot: r.ContextSnapshot, TriggerType: r.TriggerType, TriggerSource: r.TriggerSource, TriggerPayload: r.TriggerPayload, RuntimeMode: r.RuntimeMode, RuntimeClusterID: r.RuntimeClusterID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}
	if message, ok := r.Error["message"].(string); ok {
		item.Error = message
	}
	if revision, ok := r.ContextSnapshot["flow_revision"].(map[string]any); ok {
		if value, ok := revision["revision"].(float64); ok {
			item.FlowRevision = int64(value)
			item.Version = int64(value)
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

func collectAgentNames(nodes []entities.Node, result map[string]struct{}) {
	for i := range nodes {
		if nodes[i].Agent != "" {
			result[nodes[i].Agent] = struct{}{}
		}
		if nodes[i].Node != nil {
			collectAgentNames([]entities.Node{*nodes[i].Node}, result)
		}
	}
}

type ListFilter struct {
	Namespace    string
	Status       string
	RuntimeMode  string
	K8sNamespace string
	FlowID       string
	Page         entities.PageRequest
}

type MetricRequest struct {
	Namespace string
	Since     time.Time
	Until     time.Time
	Buckets   int
}

// IFlowRunStore is the agentflow run entity store interface.
type IFlowRunStore interface {
	Get(ctx context.Context, id string) (*entities.FlowRunInfo, error)
	List(ctx context.Context, filter ListFilter) (*entities.Page[entities.FlowRunInfo], error)
	Metrics(ctx context.Context, req MetricRequest) (*entities.RunMetrics, error)
	HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error)
	Save(ctx context.Context, entity *entities.FlowRunInfo) error
	Delete(ctx context.Context, id string) error
	Create(ctx context.Context, entity *entities.FlowRunInfo) error
	Update(ctx context.Context, entity *entities.FlowRunInfo) error
	Cancel(ctx context.Context, id string) error
}

type metricRow struct {
	bucket          int
	status          entities.RunStatus
	count           int64
	durationTotalMs int64
	durationCount   int64
}

func buildMetrics(req MetricRequest, rows []metricRow) *entities.RunMetrics {
	if req.Buckets < 1 || !req.Until.After(req.Since) {
		return &entities.RunMetrics{Buckets: []entities.RunMetricBucket{}}
	}
	width := req.Until.Sub(req.Since) / time.Duration(req.Buckets)
	result := &entities.RunMetrics{Buckets: make([]entities.RunMetricBucket, req.Buckets)}
	for i := range result.Buckets {
		start := req.Since.Add(time.Duration(i) * width)
		result.Buckets[i] = entities.RunMetricBucket{StartTime: start, EndTime: start.Add(width)}
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
		case entities.RunRunning:
			result.Running += row.count
			bucket.Running += row.count
		case entities.RunCompleted:
			result.Completed += row.count
			bucket.Completed += row.count
		case entities.RunFailed:
			result.Failed += row.count
			bucket.Failed += row.count
		case entities.RunCancelled:
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
