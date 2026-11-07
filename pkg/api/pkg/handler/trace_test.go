package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
)

type traceRunStore struct {
	run *entities.FlowRunInfo
}

func (s *traceRunStore) Get(context.Context, string) (*entities.FlowRunInfo, error) {
	return s.run, nil
}
func (s *traceRunStore) List(context.Context, flowrun.ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	return nil, nil
}
func (s *traceRunStore) Metrics(context.Context, flowrun.MetricRequest) (*entities.RunMetrics, error) {
	return nil, nil
}
func (s *traceRunStore) HasActiveForFlow(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *traceRunStore) Save(context.Context, *entities.FlowRunInfo) error   { return nil }
func (s *traceRunStore) Delete(context.Context, string) error                { return nil }
func (s *traceRunStore) Create(context.Context, *entities.FlowRunInfo) error { return nil }
func (s *traceRunStore) Update(context.Context, *entities.FlowRunInfo) error { return nil }
func (s *traceRunStore) Cancel(context.Context, string) error                { return nil }

type traceQueryStub struct {
	called bool
}

func (q *traceQueryStub) QueryRun(_ context.Context, runID string) (*entities.RunTrace, error) {
	q.called = true
	return &entities.RunTrace{RunID: runID, Source: "jaeger", FetchedAt: time.Now(), Traces: []entities.TraceInfo{}}, nil
}

func TestTraceHandlerEnforcesRunNamespace(t *testing.T) {
	t.Parallel()
	query := &traceQueryStub{}
	handler := newTraceHandler(&traceRunStore{run: &entities.FlowRunInfo{
		BaseEntity: entities.BaseEntity{ID: "run-1", Namespace: "tenant-a"},
	}}, query)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{run_id}/trace", handler.GetRunTrace)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenant-b/runs/run-1/trace", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	if query.called {
		t.Fatal("query backend must not be called across namespaces")
	}
}

func TestTraceHandlerReturnsNormalizedTrace(t *testing.T) {
	t.Parallel()
	query := &traceQueryStub{}
	handler := newTraceHandler(&traceRunStore{run: &entities.FlowRunInfo{
		BaseEntity: entities.BaseEntity{ID: "run-1", Namespace: "tenant-a"},
	}}, query)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{run_id}/trace", handler.GetRunTrace)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenant-a/runs/run-1/trace", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !query.called {
		t.Fatalf("status = %d, called = %v", response.Code, query.called)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}
