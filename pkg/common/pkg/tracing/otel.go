package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Provider holds the OTEL tracer and meter providers for graceful shutdown.
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
}

// NewProvider initializes OTEL tracing and metrics from config.
// The OTLP endpoint comes from otelCfg.Endpoint (env overrides are applied centrally
// by config.Load via FLOWGENT__OTEL__ENDPOINT), falling back to localhost:4317.
func NewProvider(ctx context.Context, svcName, svcVersion string, otelCfg *OTELConfig, metricsCfg *MetricsConfig) (*Provider, error) {
	endpoint := "localhost:4317"
	if otelCfg != nil && otelCfg.Endpoint != "" {
		endpoint = otelCfg.Endpoint
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure())
	if err != nil {
		return nil, err
	}

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
		sdktrace.WithBatcher(exp),
	)

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
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

// ─── Component helpers ──────────────────────────────────────────

// Meter returns a named meter for a component.
func Meter(name string) metric.Meter { return otel.Meter(name) }

// Tracer returns a named tracer for a component.
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }
