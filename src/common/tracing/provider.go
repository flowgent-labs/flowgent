package tracing

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Provider holds the OTEL tracer and meter providers for graceful shutdown.
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
}

// NewProvider initializes OTEL tracing and metrics from config.
// Actual OTLP export is configured via OTEL_EXPORTER_OTLP_ENDPOINT env var.
func NewProvider(ctx context.Context, svcName, svcVersion string, otelCfg *config.OTELConfig, metricsCfg *config.MetricsConfig) (*Provider, error) {
	var attrs []attribute.KeyValue
	attrs = append(attrs,
		attribute.String("service.name", svcName),
		attribute.String("service.version", svcVersion),
	)
	if metricsCfg != nil {
		for k, v := range metricsCfg.Labels {
			attrs = append(attrs, attribute.String(k, v))
		}
	}

	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, err
	}

	sampleRate := 1.0
	if otelCfg != nil && otelCfg.SampleRate > 0 {
		sampleRate = otelCfg.SampleRate
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
	)

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	return &Provider{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	}, nil
}

// Shutdown gracefully stops the providers.
func (p *Provider) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if p.TracerProvider != nil {
		return p.TracerProvider.Shutdown(ctx)
	}
	return nil
}
