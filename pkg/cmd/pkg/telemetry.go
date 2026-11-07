package main

import (
	"context"
	"log/slog"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// startOTELTracing centralizes component tracer lifecycle so standalone and
// distributed entrypoints export the same span contract.
func startOTELTracing(cfg *config.FlowgentConfig, serviceName string) func() {
	if cfg == nil || !cfg.Mgmt.OTEL.Enabled || cfg.Mgmt.OTEL.Endpoint == "" {
		return func() {}
	}
	otelCfg := &tracing.OTELConfig{
		Enabled:    cfg.Mgmt.OTEL.Enabled,
		Endpoint:   cfg.Mgmt.OTEL.Endpoint,
		Protocol:   cfg.Mgmt.OTEL.Protocol,
		Timeout:    cfg.Mgmt.OTEL.Timeout,
		SampleRate: cfg.Mgmt.OTEL.SampleRate,
	}
	provider, err := tracing.NewProvider(context.Background(), serviceName, "1.0", otelCfg, nil)
	if err != nil {
		slog.Warn("OTEL tracer provider init failed, tracing disabled", "service", serviceName, "error", err)
		return func() {}
	}
	slog.Info("OTEL tracing enabled", "service", serviceName, "endpoint", cfg.Mgmt.OTEL.Endpoint)
	return func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			slog.Warn("OTEL tracer provider shutdown failed", "service", serviceName, "error", err)
		}
	}
}
