package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/src/common/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"time"
)

type ExecutorMetrics struct {
	meter     metric.Meter
	TaskTotal metric.Int64Counter
	TaskDuration metric.Float64Histogram
	LLMCalls  metric.Int64Counter
	LLMLatency metric.Float64Histogram
	LLMTokens metric.Int64Counter
}

func NewExecutorMetrics() *ExecutorMetrics {
	m := &ExecutorMetrics{meter: tracing.Meter("flowgent/executor")}

	m.TaskTotal, _ = m.meter.Int64Counter("flowgent.exec.tasks.total",
		metric.WithDescription("Total task executions by type and status"))
	m.TaskDuration, _ = m.meter.Float64Histogram("flowgent.exec.tasks.duration",
		metric.WithDescription("Task execution duration (ms)"),
		metric.WithExplicitBucketBoundaries(10, 50, 100, 500, 1000, 5000, 30000, 60000))
	m.LLMCalls, _ = m.meter.Int64Counter("flowgent.exec.llm.calls",
		metric.WithDescription("LLM API calls by task type and model"))
	m.LLMLatency, _ = m.meter.Float64Histogram("flowgent.exec.llm.latency",
		metric.WithDescription("LLM round-trip duration (ms)"),
		metric.WithExplicitBucketBoundaries(100, 500, 1000, 5000, 15000, 30000, 60000, 120000))
	m.LLMTokens, _ = m.meter.Int64Counter("flowgent.exec.llm.tokens",
		metric.WithDescription("LLM token usage by direction"))
	return m
}

// RecordTask records a completed task execution.
func (m *ExecutorMetrics) RecordTask(ctx context.Context, taskType, status string, dur time.Duration) {
	m.TaskTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("task_type", taskType),
		attribute.String("status", status),
	))
	m.TaskDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(
		attribute.String("task_type", taskType),
	))
}

// RecordLLMCall records an LLM API invocation.
func (m *ExecutorMetrics) RecordLLMCall(ctx context.Context, taskType, model string, dur time.Duration, tokensIn, tokensOut int64) {
	m.LLMCalls.Add(ctx, 1, metric.WithAttributes(
		attribute.String("task_type", taskType),
		attribute.String("model", model),
	))
	m.LLMLatency.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(
		attribute.String("task_type", taskType),
		attribute.String("model", model),
	))
	if tokensIn > 0 {
		m.LLMTokens.Add(ctx, tokensIn, metric.WithAttributes(
			attribute.String("task_type", taskType),
			attribute.String("model", model),
			attribute.String("direction", "input"),
		))
	}
	if tokensOut > 0 {
		m.LLMTokens.Add(ctx, tokensOut, metric.WithAttributes(
			attribute.String("task_type", taskType),
			attribute.String("model", model),
			attribute.String("direction", "output"),
		))
	}
}
