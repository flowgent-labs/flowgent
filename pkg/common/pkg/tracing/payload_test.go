package tracing

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestPayloadAttributesContainOnlyBoundedMetadata(t *testing.T) {
	attrs := PayloadAttributes("input", map[string]any{"secret": "do-not-export"})
	values := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value
	}

	if got := values["flowgent.input.content_type"].AsString(); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := values["flowgent.input.size_bytes"].AsInt64(); got != 26 {
		t.Fatalf("size = %d", got)
	}
	if got := values["flowgent.input.sha256"].AsString(); len(got) != 64 {
		t.Fatalf("sha256 length = %d", len(got))
	}
	if got := values["flowgent.input.content_captured"].AsBool(); got {
		t.Fatal("payload content must not be captured in OTel attributes")
	}
	for key, value := range values {
		if value.Type() == attribute.STRING && value.AsString() == "do-not-export" {
			t.Fatalf("secret content leaked through %s", key)
		}
	}
}
