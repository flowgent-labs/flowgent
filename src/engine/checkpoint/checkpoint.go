package checkpoint

import (
	"github.com/flowgent-labs/flowgent/src/store"
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/src/common/tracing"
	"github.com/flowgent-labs/flowgent/src/model"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Checkpointer persists ExecutionPlan checkpoints.
// Default strategy: CheckpointPerTask — persist at task boundary.
// AgentExecutor can call Save in its inner loop for incremental checkpoints.
type Checkpointer struct {
	store store.Store
}

func NewCheckpointer(store store.Store) *Checkpointer {
	return &Checkpointer{store: store}
}

// Save persists the current checkpoint from the plan.
func (c *Checkpointer) Save(ctx context.Context, plan *model.ExecutionPlan) error {
	if plan.Checkpoint == nil {
		plan.Checkpoint = &model.TaskCheckpoint{}
	}
	plan.Checkpoint.CheckpointedAt = time.Now()
	return c.store.SaveCheckpoint(ctx, plan.PlanID, plan.Checkpoint)
}

// Load retrieves the checkpoint for a given plan, or nil if none exists.
func (c *Checkpointer) Load(ctx context.Context, planID string) (*model.TaskCheckpoint, error) {
	return c.store.LoadCheckpoint(ctx, planID)
}

// Restore restores a plan's checkpoint from the store.
func (c *Checkpointer) Restore(ctx context.Context, plan *model.ExecutionPlan) error {
	cp, err := c.store.LoadCheckpoint(ctx, plan.PlanID)
	if err != nil {
		return err
	}
	if cp != nil {
		plan.Checkpoint = cp
	}
	return nil
}

type CheckpointMetrics struct {
	meter    metric.Meter
	Saves    metric.Int64Counter
	Loads    metric.Int64Counter
	Duration metric.Float64Histogram
	Bytes    metric.Int64Histogram
}

func NewCheckpointMetrics() *CheckpointMetrics {
	m := &CheckpointMetrics{meter: tracing.Meter("flowgent/checkpoint")}

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
