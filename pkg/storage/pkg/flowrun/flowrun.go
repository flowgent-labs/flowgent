package flowrun

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

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
