package engine

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type CheckpointMetrics struct {
	meter    metric.Meter
	Saves    metric.Int64Counter
	Loads    metric.Int64Counter
	Duration metric.Float64Histogram
	Bytes    metric.Int64Histogram
}

func NewCheckpointMetrics() *CheckpointMetrics {
	m := &CheckpointMetrics{meter: otel.Meter("flowgent/checkpoint")}

	m.Saves, _ = m.meter.Int64Counter("flowgent.checkpoint.saves",
		metric.WithDescription("Checkpoint save operations"))
	m.Loads, _ = m.meter.Int64Counter("flowgent.checkpoint.loads",
		metric.WithDescription("Checkpoint restore operations"))
	m.Duration, _ = m.meter.Float64Histogram("flowgent.checkpoint.duration",
		metric.WithDescription("Save/load duration (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000))
	m.Bytes, _ = m.meter.Int64Histogram("flowgent.checkpoint.bytes",
		metric.WithDescription("Checkpoint payload size (bytes)"),
		metric.WithExplicitBucketBoundaries(128, 512, 1024, 4096, 16384, 65536, 262144))
	return m
}

func (m *CheckpointMetrics) RecordSave(ctx context.Context, taskType string, dur time.Duration, sizeBytes int64) {
	m.Saves.Add(ctx, 1, metric.WithAttributes(attribute.String("task_type", taskType)))
	m.Duration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attribute.String("task_type", taskType), attribute.String("op", "save")))
	m.Bytes.Record(ctx, sizeBytes, metric.WithAttributes(attribute.String("task_type", taskType)))
}

func (m *CheckpointMetrics) RecordLoad(ctx context.Context, taskType string, dur time.Duration) {
	m.Loads.Add(ctx, 1, metric.WithAttributes(attribute.String("task_type", taskType)))
	m.Duration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attribute.String("task_type", taskType), attribute.String("op", "load")))
}
