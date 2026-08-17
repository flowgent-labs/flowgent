package tracing

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
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
// by config.Load via FLOWGENT__OTEL__ENDPOINT), falling back to the standard
// OTLP/HTTP endpoint at localhost:4318.
func NewProvider(ctx context.Context, svcName, svcVersion string, otelCfg *OTELConfig, metricsCfg *MetricsConfig) (*Provider, error) {
	endpoint, exportTimeout, err := normalizeOTLPHTTPConfig(otelCfg)
	if err != nil {
		return nil, err
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithTimeout(exportTimeout))
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
	_, startupSpan := tracerProvider.Tracer("flowgent/otel").Start(ctx, "otel.startup")
	startupSpan.End()
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := tracerProvider.ForceFlush(flushCtx); err != nil {
		_ = tracerProvider.Shutdown(context.Background())
		return nil, err
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Provider{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	}, nil
}

func normalizeOTLPHTTPConfig(cfg *OTELConfig) (string, time.Duration, error) {
	protocol := "http/protobuf"
	endpoint := "http://localhost:4318"
	timeout := 10 * time.Second
	if cfg != nil {
		if value := strings.ToLower(strings.TrimSpace(cfg.Protocol)); value != "" {
			protocol = value
		}
		if value := strings.TrimSpace(cfg.Endpoint); value != "" {
			endpoint = value
		}
		if cfg.Timeout > 0 {
			timeout = time.Duration(cfg.Timeout) * time.Millisecond
		}
	}
	if protocol != "http" && protocol != "http/protobuf" && protocol != "http-protobuf" {
		return "", 0, fmt.Errorf("unsupported OTLP trace protocol %q; supported: http/protobuf", protocol)
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", 0, fmt.Errorf("invalid OTLP/HTTP trace endpoint %q", endpoint)
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1/traces"
	}
	return parsed.String(), timeout, nil
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
