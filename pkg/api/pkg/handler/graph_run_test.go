package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/nodepayload"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
)

type graphRunStoreStub struct {
	runs    map[string]*entities.GraphRun
	page    *entities.Page[entities.GraphRun]
	updated *entities.GraphRun
	filter  graphrun.ListFilter
}

func (s *graphRunStoreStub) Get(_ context.Context, id string) (*entities.GraphRun, error) {
	if run := s.runs[id]; run != nil {
		return run, nil
	}
	return nil, sql.ErrNoRows
}
func (s *graphRunStoreStub) List(_ context.Context, filter graphrun.ListFilter) (*entities.Page[entities.GraphRun], error) {
	s.filter = filter
	return s.page, nil
}
func (s *graphRunStoreStub) Metrics(context.Context, graphrun.MetricRequest) (*entities.GraphRunMetrics, error) {
	return &entities.GraphRunMetrics{Buckets: []entities.GraphRunMetricBucket{}}, nil
}
func (s *graphRunStoreStub) HasActiveForAgentGraph(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *graphRunStoreStub) Save(context.Context, *entities.GraphRun) error   { return nil }
func (s *graphRunStoreStub) Delete(context.Context, string) error             { return nil }
func (s *graphRunStoreStub) Create(context.Context, *entities.GraphRun) error { return nil }
func (s *graphRunStoreStub) Update(_ context.Context, run *entities.GraphRun) error {
	copy := *run
	s.updated = &copy
	return nil
}
func (s *graphRunStoreStub) Cancel(context.Context, string) error { return nil }

type taskStoreStub struct {
	tasks      map[string]*entities.NodeRun
	updated    *entities.NodeRun
	listCalled bool
}

func (s *taskStoreStub) Get(_ context.Context, id string) (*entities.NodeRun, error) {
	if noderun := s.tasks[id]; noderun != nil {
		return noderun, nil
	}
	return nil, sql.ErrNoRows
}
func (s *taskStoreStub) Select(context.Context, entities.PageRequest) (*entities.Page[entities.NodeRun], error) {
	return nil, nil
}
func (s *taskStoreStub) Save(context.Context, *entities.NodeRun) error { return nil }
func (s *taskStoreStub) Delete(context.Context, string) error          { return nil }
func (s *taskStoreStub) GetByExecutionID(context.Context, string) (*entities.NodeRun, error) {
	return nil, sql.ErrNoRows
}
func (s *taskStoreStub) CreateNodeRun(context.Context, *entities.NodeRun) error { return nil }
func (s *taskStoreStub) UpdateNodeRun(_ context.Context, noderun *entities.NodeRun) error {
	copy := *noderun
	s.updated = &copy
	return nil
}
func (s *taskStoreStub) ListByGraphRun(context.Context, string) ([]*entities.NodeRun, error) {
	s.listCalled = true
	return nil, nil
}

func TestGraphRunListDoesNotDiscloseOtherNamespaces(t *testing.T) {
	t.Parallel()
	tenantA := &entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}}
	store := &graphRunStoreStub{page: &entities.Page[entities.GraphRun]{
		Items: []*entities.GraphRun{tenantA}, TotalCount: 1,
	}}
	handler := &GraphRunHandler{runStore: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs", handler.List)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-a/graph-runs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var page entities.Page[entities.GraphRun]
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "run-a" {
		t.Fatalf("items = %+v", page.Items)
	}
	if store.filter.Namespace != "tenant-a" {
		t.Fatalf("database filter namespace = %q", store.filter.Namespace)
	}
}

func TestGraphRunLifecycleUpdatePersistsJobMasterTimestamps(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Minute)
	store := &graphRunStoreStub{runs: map[string]*entities.GraphRun{
		"run-a": {
			BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"},
			Status:     entities.GraphRunPending,
		},
	}}
	handler := &GraphRunHandler{runStore: store}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}", handler.Update)

	body := `{"status":"COMPLETED","started_at":"2026-08-16T00:00:00Z","finished_at":"2026-08-16T00:03:00Z"}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/graph-runs/run-a", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.updated == nil || store.updated.Status != entities.GraphRunCompleted {
		t.Fatalf("updated run = %+v", store.updated)
	}
	if !store.updated.StartedAt.Equal(started) || !store.updated.FinishedAt.Equal(finished) {
		t.Fatalf("timestamps = %v / %v", store.updated.StartedAt, store.updated.FinishedAt)
	}
}

func TestGraphRunLifecycleRejectsInvertedTimestamps(t *testing.T) {
	t.Parallel()
	store := &graphRunStoreStub{runs: map[string]*entities.GraphRun{
		"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
	}}
	handler := &GraphRunHandler{runStore: store}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}", handler.Update)

	body := `{"status":"FAILED","started_at":"2026-08-16T00:03:00Z","finished_at":"2026-08-16T00:00:00Z"}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/graph-runs/run-a", strings.NewReader(body)))
	if response.Code != http.StatusBadRequest || store.updated != nil {
		t.Fatalf("status = %d, updated = %+v", response.Code, store.updated)
	}
}

func TestGraphRunTasksRequireRunNamespaceOwnership(t *testing.T) {
	t.Parallel()
	tasks := &taskStoreStub{}
	handler := &GraphRunHandler{
		runStore: &graphRunStoreStub{runs: map[string]*entities.GraphRun{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
		}},
		taskStore: tasks,
		payloads:  nodepayload.NewDefaultNodePayloadProvider(0),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs", handler.ListNodeRuns)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-b/graph-runs/run-a/node-runs", nil))
	if response.Code != http.StatusNotFound || tasks.listCalled {
		t.Fatalf("status = %d, list called = %v", response.Code, tasks.listCalled)
	}
}

func TestUpdateTaskUsesOwnedRunFromPath(t *testing.T) {
	t.Parallel()
	tasks := &taskStoreStub{tasks: map[string]*entities.NodeRun{}}
	handler := &GraphRunHandler{
		runStore: &graphRunStoreStub{runs: map[string]*entities.GraphRun{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
		}},
		taskStore: tasks,
		payloads:  nodepayload.NewDefaultNodePayloadProvider(0),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}", handler.UpdateNodeRun)

	body := `{"graph_run_id":"run-b","namespace_id":"tenant-b","node_instance_id":"node-a","status":"SUCCESS","attempt":1}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/graph-runs/run-a/node-runs/node-run-a", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if tasks.updated == nil || tasks.updated.GraphRunID != "run-a" || tasks.updated.Namespace != "tenant-a" || tasks.updated.NodeInstanceID != "node-a" {
		t.Fatalf("updated noderun = %+v", tasks.updated)
	}
}

func TestUpdateNodeRunRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	handler := &GraphRunHandler{
		runStore: &graphRunStoreStub{runs: map[string]*entities.GraphRun{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
		}},
		taskStore: &taskStoreStub{tasks: map[string]*entities.NodeRun{}},
		payloads:  nodepayload.NewDefaultNodePayloadProvider(0),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}", handler.UpdateNodeRun)
	body := `{"unexpected_field":"run-a","node_instance_id":"node-a","status":"SUCCESS"}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/graph-runs/run-a/node-runs/node-run-a", strings.NewReader(body)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
