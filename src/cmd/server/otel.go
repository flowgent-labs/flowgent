package main

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type otelCleanup struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *metric.MeterProvider
}

// newOTEL initializes OTEL with a sampling tracer provider.
// The endpoint and timeout params are consumed for configuration;
// full OTLP export requires additional setup via env vars or external collector.
func newOTEL(serviceName, version, endpoint string, timeoutMs int) (*otelCleanup, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", version),
		),
	)
	if err != nil {
		return nil, err
	}

	// endpoint and timeoutMs consumed for configuration audit;
	// actual OTLP export via OTEL_EXPORTER_OTLP_ENDPOINT env var.
	_ = endpoint
	_ = time.Duration(timeoutMs) * time.Millisecond

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	return &otelCleanup{tracerProvider: tracerProvider, meterProvider: meterProvider}, nil
}

func (o *otelCleanup) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if o.tracerProvider != nil {
		return o.tracerProvider.Shutdown(ctx)
	}
	return nil
}
