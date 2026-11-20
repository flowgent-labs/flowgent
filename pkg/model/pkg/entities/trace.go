package entities

import "time"

// RunTrace is the normalized tracing response returned by the API Server.
// It intentionally models OpenTelemetry concepts instead of exposing the
// storage-specific Jaeger Query API response to clients.
type RunTrace struct {
	RunID     string      `json:"run_id"`
	Source    string      `json:"source"`
	FetchedAt time.Time   `json:"fetched_at"`
	Traces    []TraceInfo `json:"traces"`
}

// TraceInfo contains all spans that belong to one W3C trace.
type TraceInfo struct {
	TraceID        string      `json:"trace_id"`
	RootSpanID     string      `json:"root_span_id,omitempty"`
	StartTime      time.Time   `json:"start_time"`
	DurationMicros int64       `json:"duration_micros"`
	Services       []string    `json:"services"`
	Spans          []TraceSpan `json:"spans"`
}

// TraceSpan is the backend-neutral span shape consumed by the web console.
// Task input/output remains in TaskRunInfo and is correlated through the
// flowgent.task_id and flowgent.attempt attributes.
type TraceSpan struct {
	TraceID        string         `json:"trace_id"`
	SpanID         string         `json:"span_id"`
	ParentSpanID   string         `json:"parent_span_id,omitempty"`
	OperationName  string         `json:"operation_name"`
	ServiceName    string         `json:"service_name"`
	StartTime      time.Time      `json:"start_time"`
	DurationMicros int64          `json:"duration_micros"`
	Status         string         `json:"status"`
	Kind           string         `json:"kind,omitempty"`
	Attributes     map[string]any `json:"attributes"`
	Events         []TraceEvent   `json:"events,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
}

// TraceEvent represents an OpenTelemetry span event as returned by Jaeger.
type TraceEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Name       string         `json:"name,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}
