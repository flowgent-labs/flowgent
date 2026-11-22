package tracing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelexport "go.opentelemetry.io/otel/exporters/prometheus"
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
	MetricsHandler http.Handler
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

	traceOptions := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
	}
	traceEnabled := otelCfg == nil || otelCfg.Enabled
	if traceEnabled {
		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(endpoint),
			otlptracehttp.WithTimeout(exportTimeout))
		if err != nil {
			return nil, err
		}
		traceOptions = append(traceOptions, sdktrace.WithBatcher(exp))
	} else {
		traceOptions = append(traceOptions, sdktrace.WithSampler(sdktrace.NeverSample()))
	}
	tracerProvider := sdktrace.NewTracerProvider(traceOptions...)
	if traceEnabled {
		_, startupSpan := tracerProvider.Tracer("flowgent/otel").Start(ctx, "otel.startup")
		startupSpan.End()
		// The batch exporter retries asynchronously. Collector startup must not
		// disable the service's metrics or block control-plane readiness.
	}

	meterOptions := []sdkmetric.Option{sdkmetric.WithResource(res)}
	var metricsHandler http.Handler
	if metricsCfg != nil && metricsCfg.Enabled && metricsCfg.Prometheus {
		registry := prometheus.NewRegistry()
		// Keep query names stable when Prometheus negotiates UTF-8 exposition.
		exporter, err := otelexport.New(
			otelexport.WithRegisterer(registry),
			otelexport.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		)
		if err != nil {
			_ = tracerProvider.Shutdown(context.Background())
			return nil, err
		}
		meterOptions = append(meterOptions, sdkmetric.WithReader(exporter))
		metricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	}

	meterProvider := sdkmetric.NewMeterProvider(meterOptions...)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Provider{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
		MetricsHandler: metricsHandler,
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
	var errs []error
	if p.MeterProvider != nil {
		errs = append(errs, p.MeterProvider.Shutdown(ctx))
	}
	if p.TracerProvider != nil {
		errs = append(errs, p.TracerProvider.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

// ─── Component helpers ──────────────────────────────────────────

// Meter returns a named meter for a component.
func Meter(name string) metric.Meter { return otel.Meter(name) }

// Tracer returns a named tracer for a component.
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }
