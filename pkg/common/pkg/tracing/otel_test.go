package tracing

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestPrometheusExportsRealOTelCountersWithoutTraceCollector(t *testing.T) {
	provider, err := NewProvider(t.Context(), "fixture", "test", &OTELConfig{Enabled: false}, &MetricsConfig{Enabled: true, Prometheus: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
	})
	counter, err := Meter("fixture").Int64Counter("flowgent.fixture.executions")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(t.Context(), 3, metric.WithAttributes(attribute.String("agent_graph_name", "repair")))
	response := httptest.NewRecorder()
	provider.MetricsHandler.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `flowgent_fixture_executions_total{`) || !strings.Contains(response.Body.String(), `agent_graph_name="repair"`) || !strings.Contains(response.Body.String(), `} 3`) {
		t.Fatalf("OTel counter was not exported: %s", response.Body.String())
	}
}

func TestNormalizeOTLPHTTPConfig(t *testing.T) {
	endpoint, timeout, err := normalizeOTLPHTTPConfig(nil)
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	if endpoint != "http://localhost:4318/v1/traces" || timeout != 10*time.Second {
		t.Fatalf("default endpoint = %q, timeout = %s", endpoint, timeout)
	}

	endpoint, timeout, err = normalizeOTLPHTTPConfig(&OTELConfig{
		Endpoint: "otel.example.com:4318",
		Protocol: "http/protobuf",
		Timeout:  2500,
	})
	if err != nil {
		t.Fatalf("explicit config: %v", err)
	}
	if endpoint != "http://otel.example.com:4318/v1/traces" || timeout != 2500*time.Millisecond {
		t.Fatalf("explicit endpoint = %q, timeout = %s", endpoint, timeout)
	}
}

func TestNormalizeOTLPHTTPConfigRejectsSilentlyMisroutedProtocols(t *testing.T) {
	if _, _, err := normalizeOTLPHTTPConfig(&OTELConfig{Protocol: "grpc"}); err == nil {
		t.Fatal("expected unsupported grpc protocol error")
	}
	if _, _, err := normalizeOTLPHTTPConfig(&OTELConfig{Endpoint: "ftp://collector"}); err == nil {
		t.Fatal("expected invalid endpoint error")
	}
}
