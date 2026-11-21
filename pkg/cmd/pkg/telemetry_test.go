package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestManagementExportsMetricsWhenTracingIsDisabled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cfg := &config.FlowgentConfig{Mgmt: config.MgmtConfig{
		Enabled: true, Host: "127.0.0.1", Port: port,
		Metrics: config.MetricsConfig{Enabled: true, Prometheus: true},
	}}
	stop := startOTELTracing(cfg, "fixture")
	t.Cleanup(func() {
		stop()
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
	})
	counter, err := tracing.Meter("fixture").Int64Counter("flowgent.management.probe")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 2)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "flowgent_management_probe_total") {
		t.Fatalf("management metrics unavailable: %d %s, %v", response.StatusCode, body, err)
	}
}
