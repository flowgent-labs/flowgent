package engine

import (
	"github.com/flowgent-labs/flowgent/src/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type TaskManagerMetrics struct {
	meter          metric.Meter
	SlotsBusy      metric.Int64UpDownCounter
	TasksExecuted  metric.Int64Counter
	TaskDuration   metric.Float64Histogram
	DequeueLatency metric.Float64Histogram
}

func NewTaskManagerMetrics() *TaskManagerMetrics {
	m := &TaskManagerMetrics{meter: otel.Meter("flowgent/taskmanager")}

	m.SlotsBusy, _ = m.meter.Int64UpDownCounter("flowgent.tm.slots.busy",
		metric.WithDescription("Currently occupied slots"))
	m.TasksExecuted, _ = m.meter.Int64Counter("flowgent.tm.tasks.executed",
		metric.WithDescription("Total tasks executed"))
	m.TaskDuration, _ = m.meter.Float64Histogram("flowgent.tm.tasks.duration",
		metric.WithDescription("Task wall-clock time (ms)"),
		metric.WithExplicitBucketBoundaries(10, 50, 100, 500, 1000, 5000, 30000, 60000))
	m.DequeueLatency, _ = m.meter.Float64Histogram("flowgent.tm.dequeue.latency",
		metric.WithDescription("MQTT dequeue→acquire time (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000))
	return m
}

func taskTypeAttr(t model.TaskType) attribute.KeyValue {
	return attribute.String("task_type", string(t))
}
