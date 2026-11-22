package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// startOTELTracing owns tracing, the existing OTel meter provider and its
// private management listener for each standalone or distributed process.
func startOTELTracing(cfg *config.FlowgentConfig, serviceName string) func() {
	if cfg == nil {
		return func() {}
	}
	otelCfg := &tracing.OTELConfig{
		Enabled:    cfg.Mgmt.OTEL.Enabled && cfg.Mgmt.OTEL.Endpoint != "",
		Endpoint:   cfg.Mgmt.OTEL.Endpoint,
		Protocol:   cfg.Mgmt.OTEL.Protocol,
		Timeout:    cfg.Mgmt.OTEL.Timeout,
		SampleRate: cfg.Mgmt.OTEL.SampleRate,
	}
	metricsCfg := &tracing.MetricsConfig{
		Enabled:    cfg.Mgmt.Enabled && cfg.Mgmt.Metrics.Enabled,
		Prometheus: cfg.Mgmt.Metrics.Prometheus,
		Labels:     cfg.Mgmt.Metrics.Labels,
	}
	if !otelCfg.Enabled && !metricsCfg.Enabled {
		return func() {}
	}
	provider, err := tracing.NewProvider(context.Background(), serviceName, "1.0", otelCfg, metricsCfg)
	if err != nil {
		slog.Warn("OTEL tracer provider init failed, tracing disabled", "service", serviceName, "error", err)
		return func() {}
	}
	var management *http.Server
	if provider.MetricsHandler != nil {
		port := cfg.Mgmt.Port
		if port <= 0 {
			port = 9991
		}
		listener, listenErr := net.Listen("tcp", net.JoinHostPort(cfg.Mgmt.Host, strconv.Itoa(port)))
		if listenErr != nil {
			slog.Error("metrics management listener failed", "service", serviceName, "error", listenErr)
		} else {
			mux := http.NewServeMux()
			mux.Handle("GET /metrics", provider.MetricsHandler)
			mux.HandleFunc("GET /_/healthz", (&handler.HealthHandler{}).Healthz)
			management = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second}
			go func() { _ = management.Serve(listener) }()
			slog.Info("metrics management listener started", "service", serviceName, "address", listener.Addr())
		}
	}
	slog.Info("OTEL telemetry enabled", "service", serviceName, "tracing", otelCfg.Enabled, "prometheus", provider.MetricsHandler != nil)
	return func() {
		if management != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = management.Shutdown(ctx)
			cancel()
		}
		if err := provider.Shutdown(context.Background()); err != nil {
			slog.Warn("OTEL tracer provider shutdown failed", "service", serviceName, "error", err)
		}
	}
}
