package messager

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// InstrumentedMessager wraps a Messager with OTEL metric instrumentation.
type InstrumentedMessager struct {
	inner          IMessager
	publishTotal   metric.Int64Counter
	ackTotal       metric.Int64Counter
	nackTotal      metric.Int64Counter
	handleLatency  metric.Float64Histogram
}

func NewInstrumentedMessager(inner IMessager) *InstrumentedMessager {
	meter := tracing.Meter("flowgent/messaging")
	m := &InstrumentedMessager{inner: inner}
	m.publishTotal, _ = meter.Int64Counter("flowgent.messager.publish.total",
		metric.WithDescription("Total messages published"))
	m.ackTotal, _ = meter.Int64Counter("flowgent.messager.ack.total",
		metric.WithDescription("Successful acks"))
	m.nackTotal, _ = meter.Int64Counter("flowgent.messager.nack.total",
		metric.WithDescription("Failed → requeue"))
	m.handleLatency, _ = meter.Float64Histogram("flowgent.messager.handle.latency",
		metric.WithDescription("Message handler latency (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000))
	return m
}

func (m *InstrumentedMessager) Publish(ctx context.Context, topic string, msg *InterMessage) error {
	err := m.inner.Publish(ctx, topic, msg)
	if err == nil {
		m.publishTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", topic)))
	}
	return err
}

func (m *InstrumentedMessager) Subscribe(ctx context.Context, topic string, handler SubHandler) error {
	wrapped := func(topic string, payload []byte) {
		start := time.Now()
		handler(topic, payload)
		m.handleLatency.Record(ctx, float64(time.Since(start).Milliseconds()),
			metric.WithAttributes(attribute.String("topic", topic)))
	}
	return m.inner.Subscribe(ctx, topic, wrapped)
}

func (m *InstrumentedMessager) Ack(ctx context.Context, msgID string) error {
	err := m.inner.Ack(ctx, msgID)
	if err == nil {
		m.ackTotal.Add(ctx, 1)
	}
	return err
}

func (m *InstrumentedMessager) Nack(ctx context.Context, msgID string) error {
	err := m.inner.Nack(ctx, msgID)
	if err == nil {
		m.nackTotal.Add(ctx, 1)
	}
	return err
}

func (m *InstrumentedMessager) Close() error { return m.inner.Close() }
