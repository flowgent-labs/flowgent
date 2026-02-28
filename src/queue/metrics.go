package queue

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/src/common/tracing"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/attribute"
	
)

// MetricsQueue wraps a Queue with metric instrumentation.
type MetricsQueue struct {
	inner          Queue
	Depth          metric.Int64UpDownCounter
	PushTotal      metric.Int64Counter
	DequeueTotal   metric.Int64Counter
	AckTotal       metric.Int64Counter
	NackTotal      metric.Int64Counter
	DequeueLatency metric.Float64Histogram
}

func NewMetricsQueue(inner Queue) *MetricsQueue {
	meter := tracing.Meter("flowgent/queue")
	mq := &MetricsQueue{inner: inner}

	mq.Depth, _ = meter.Int64UpDownCounter("flowgent.queue.depth",
		metric.WithDescription("Messages in queue (push - ack)"))
	mq.PushTotal, _ = meter.Int64Counter("flowgent.queue.push.total",
		metric.WithDescription("Total messages pushed"))
	mq.DequeueTotal, _ = meter.Int64Counter("flowgent.queue.dequeue.total",
		metric.WithDescription("Total messages dequeued"))
	mq.AckTotal, _ = meter.Int64Counter("flowgent.queue.ack.total",
		metric.WithDescription("Successful acks"))
	mq.NackTotal, _ = meter.Int64Counter("flowgent.queue.nack.total",
		metric.WithDescription("Failed -> requeue events"))
	mq.DequeueLatency, _ = meter.Float64Histogram("flowgent.queue.dequeue.latency",
		metric.WithDescription("Time spent in queue before pickup (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000))
	return mq
}

func (mq *MetricsQueue) Push(ctx context.Context, msg *Message) error {
	err := mq.inner.Push(ctx, msg)
	if err == nil {
		mq.Depth.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
		mq.PushTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
	}
	return err
}

func (mq *MetricsQueue) Pop(ctx context.Context, timeout time.Duration) (*Message, error) {
	start := time.Now()
	msg, err := mq.inner.Pop(ctx, timeout)
	if err == nil && msg != nil {
		mq.DequeueLatency.Record(ctx, float64(time.Since(start).Milliseconds()),
			metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
	}
	return msg, err
}

func (mq *MetricsQueue) Dequeue(ctx context.Context, consumerGroup string) (*Message, error) {
	start := time.Now()
	msg, err := mq.inner.Dequeue(ctx, consumerGroup)
	if err == nil && msg != nil {
		mq.DequeueTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("topic", "flowgent/exec"),
				attribute.String("consumer_group", consumerGroup),
			))
		mq.DequeueLatency.Record(ctx, float64(time.Since(start).Milliseconds()),
			metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
	}
	return msg, err
}

func (mq *MetricsQueue) Ack(ctx context.Context, msgID string) error {
	err := mq.inner.Ack(ctx, msgID)
	if err == nil {
		mq.Depth.Add(ctx, -1, metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
		mq.AckTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
	}
	return err
}

func (mq *MetricsQueue) Nack(ctx context.Context, msgID string) error {
	err := mq.inner.Nack(ctx, msgID)
	if err == nil {
		mq.NackTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", "flowgent/exec")))
	}
	return err
}

func (mq *MetricsQueue) PublishHeartbeat(ctx context.Context, hb *Heartbeat) error {
	return mq.inner.PublishHeartbeat(ctx, hb)
}

func (mq *MetricsQueue) ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*Heartbeat, error) {
	return mq.inner.ConsumeHeartbeat(ctx, timeout)
}

func (mq *MetricsQueue) Close() error { return mq.inner.Close() }
