package tracing

import (
	"testing"
	"time"
)

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
