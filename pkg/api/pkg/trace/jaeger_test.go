package trace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJaegerClientQueryRunNormalizesAndFilters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("service"); got != "flowgent-jobmanager" {
			t.Fatalf("service = %q", got)
		}
		var tags map[string]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("tags")), &tags); err != nil {
			t.Fatalf("decode tags: %v", err)
		}
		if tags["run.id"] != "run-1" {
			t.Fatalf("run tag = %q", tags["run.id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "data": [
            {
              "traceID": "trace-1",
              "spans": [
                {
                  "traceID": "trace-1",
                  "spanID": "root",
                  "operationName": "jobmaster.execute",
                  "references": [],
                  "startTime": 1000000,
                  "duration": 2500,
                  "tags": [
                    {"key":"run.id","type":"string","value":"run-1"},
                    {"key":"otel.status_code","type":"string","value":"OK"}
                  ],
                  "logs": [],
                  "processID": "p1",
                  "warnings": null
                },
                {
                  "traceID": "trace-1",
                  "spanID": "child",
                  "operationName": "jobmaster.node.attempt",
                  "references": [{"refType":"CHILD_OF","traceID":"trace-1","spanID":"root"}],
                  "startTime": 1000500,
                  "duration": 500,
                  "tags": [
                    {"key":"flowgent.node_id","type":"string","value":"review"},
                    {"key":"error","type":"bool","value":true}
                  ],
                  "logs": [{"timestamp":1000900,"fields":[{"key":"event","type":"string","value":"exception"}]}],
                  "processID": "p2",
                  "warnings": ["sample warning"]
                }
              ],
              "processes": {
                "p1":{"serviceName":"flowgent-jobmanager","tags":[]},
                "p2":{"serviceName":"flowgent-taskmanager","tags":[]}
              }
            },
            {
              "traceID": "trace-other",
              "spans": [{
                "traceID":"trace-other","spanID":"other","operationName":"jobmaster.execute",
                "references":[],"startTime":2000000,"duration":1,
                "tags":[{"key":"run.id","type":"string","value":"run-other"}],
                "logs":[],"processID":"p1","warnings":null
              }],
              "processes":{"p1":{"serviceName":"flowgent-jobmanager","tags":[]}}
            }
          ]
        }`))
	}))
	defer server.Close()

	client, err := NewJaegerClient(server.URL, time.Second, "3h", 10)
	if err != nil {
		t.Fatalf("NewJaegerClient: %v", err)
	}
	result, err := client.QueryRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("QueryRun: %v", err)
	}
	if len(result.Traces) != 1 {
		t.Fatalf("trace count = %d", len(result.Traces))
	}
	trace := result.Traces[0]
	if trace.RootSpanID != "root" || trace.DurationMicros != 2500 {
		t.Fatalf("unexpected trace summary: %+v", trace)
	}
	if len(trace.Services) != 2 || len(trace.Spans) != 2 {
		t.Fatalf("unexpected normalized trace: %+v", trace)
	}
	child := trace.Spans[1]
	if child.ParentSpanID != "root" || child.Status != "ERROR" || child.Kind != "" {
		t.Fatalf("unexpected child span: %+v", child)
	}
	if len(child.Events) != 1 || child.Events[0].Name != "exception" {
		t.Fatalf("unexpected events: %+v", child.Events)
	}
}

func TestNewJaegerClientRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()
	if _, err := NewJaegerClient("grpc://jaeger:16686", 0, "", 0); err == nil {
		t.Fatal("expected invalid scheme error")
	}
}
