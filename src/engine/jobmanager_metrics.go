package engine

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type JobManagerMetrics struct {
	meter            metric.Meter
	RunsTotal        metric.Int64Counter
	RunsActive       metric.Int64UpDownCounter
	PlansDispatched  metric.Int64Counter
	PlansCompleted   metric.Int64Counter
	FailoverEvents   metric.Int64Counter
	DAGBuildDuration metric.Float64Histogram
}

func NewJobManagerMetrics() *JobManagerMetrics {
	m := &JobManagerMetrics{meter: otel.Meter("flowgent/jobmanager")}

	m.RunsTotal, _ = m.meter.Int64Counter("flowgent.jm.runs.total",
		metric.WithDescription("Total agentflow runs started/finished"))
	m.RunsActive, _ = m.meter.Int64UpDownCounter("flowgent.jm.runs.active",
		metric.WithDescription("Currently running agentflows"))
	m.PlansDispatched, _ = m.meter.Int64Counter("flowgent.jm.plans.dispatched",
		metric.WithDescription("ExecutionPlans enqueued to queue"))
	m.PlansCompleted, _ = m.meter.Int64Counter("flowgent.jm.plans.completed",
		metric.WithDescription("Plans that reached terminal state"))
	m.FailoverEvents, _ = m.meter.Int64Counter("flowgent.jm.failover.events",
		metric.WithDescription("TM failure → plan requeue events"))
	m.DAGBuildDuration, _ = m.meter.Float64Histogram("flowgent.jm.dag.build.duration",
		metric.WithDescription("DAG parsing + plan creation time (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500))
	return m
}

func (m *JobManagerMetrics) RecordRunStarted(ctx context.Context, agentFlowID string) {
	m.RunsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID), attribute.String("status", "RUNNING")))
	m.RunsActive.Add(ctx, 1)
}

func (m *JobManagerMetrics) RecordRunCompleted(ctx context.Context, agentFlowID, status string) {
	m.RunsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID), attribute.String("status", status)))
	m.RunsActive.Add(ctx, -1)
}

func (m *JobManagerMetrics) RecordPlanDispatched(ctx context.Context, agentFlowID string) {
	m.PlansDispatched.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
}

func (m *JobManagerMetrics) RecordPlanCompleted(ctx context.Context, agentFlowID, status string) {
	m.PlansCompleted.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID), attribute.String("status", status)))
}

func (m *JobManagerMetrics) RecordFailover(ctx context.Context, tmID string) {
	m.FailoverEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("tm_id", tmID)))
}

func (m *JobManagerMetrics) RecordDAGBuild(ctx context.Context, agentFlowID string, dur time.Duration) {
	m.DAGBuildDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
}
