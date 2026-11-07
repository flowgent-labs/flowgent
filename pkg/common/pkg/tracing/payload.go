package tracing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"go.opentelemetry.io/otel/attribute"
)

// PayloadAttributes returns bounded, non-content metadata for a JSON payload.
//
// Runtime input and output belong in the TaskRun persistence layer. Keeping
// their content out of span attributes avoids leaking secrets and prevents
// high-cardinality payloads from overwhelming the tracing backend. The hash
// and byte count are sufficient to correlate and verify the persisted value.
func PayloadAttributes(name string, payload any) []attribute.KeyValue {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return []attribute.KeyValue{
			attribute.Bool("flowgent."+name+".metadata_error", true),
		}
	}

	digest := sha256.Sum256(encoded)
	return []attribute.KeyValue{
		attribute.String("flowgent."+name+".content_type", "application/json"),
		attribute.Int("flowgent."+name+".size_bytes", len(encoded)),
		attribute.String("flowgent."+name+".sha256", hex.EncodeToString(digest[:])),
		attribute.Bool("flowgent."+name+".content_captured", false),
	}
}
