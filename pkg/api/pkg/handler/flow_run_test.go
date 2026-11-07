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

	"github.com/flowgent-labs/flowgent/api/pkg/taskpayload"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
)

type flowRunStoreStub struct {
	runs    map[string]*entities.FlowRunInfo
	page    *entities.Page[entities.FlowRunInfo]
	updated *entities.FlowRunInfo
	filter  flowrun.ListFilter
}

func (s *flowRunStoreStub) Get(_ context.Context, id string) (*entities.FlowRunInfo, error) {
	if run := s.runs[id]; run != nil {
		return run, nil
	}
	return nil, sql.ErrNoRows
}
func (s *flowRunStoreStub) List(_ context.Context, filter flowrun.ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	s.filter = filter
	return s.page, nil
}
func (s *flowRunStoreStub) Metrics(context.Context, flowrun.MetricRequest) (*entities.RunMetrics, error) {
	return &entities.RunMetrics{Buckets: []entities.RunMetricBucket{}}, nil
}
func (s *flowRunStoreStub) HasActiveForFlow(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *flowRunStoreStub) Save(context.Context, *entities.FlowRunInfo) error   { return nil }
func (s *flowRunStoreStub) Delete(context.Context, string) error                { return nil }
func (s *flowRunStoreStub) Create(context.Context, *entities.FlowRunInfo) error { return nil }
func (s *flowRunStoreStub) Update(_ context.Context, run *entities.FlowRunInfo) error {
	copy := *run
	s.updated = &copy
	return nil
}
func (s *flowRunStoreStub) Cancel(context.Context, string) error { return nil }

type taskStoreStub struct {
	tasks      map[string]*entities.TaskRunInfo
	updated    *entities.TaskRunInfo
	listCalled bool
}

func (s *taskStoreStub) Get(_ context.Context, id string) (*entities.TaskRunInfo, error) {
	if task := s.tasks[id]; task != nil {
		return task, nil
	}
	return nil, sql.ErrNoRows
}
func (s *taskStoreStub) Select(context.Context, entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error) {
	return nil, nil
}
func (s *taskStoreStub) Save(context.Context, *entities.TaskRunInfo) error { return nil }
func (s *taskStoreStub) Delete(context.Context, string) error              { return nil }
func (s *taskStoreStub) GetByExecID(context.Context, string) (*entities.TaskRunInfo, error) {
	return nil, sql.ErrNoRows
}
func (s *taskStoreStub) CreateTaskRun(context.Context, *entities.TaskRunInfo) error { return nil }
func (s *taskStoreStub) UpdateTaskRun(_ context.Context, task *entities.TaskRunInfo) error {
	copy := *task
	s.updated = &copy
	return nil
}
func (s *taskStoreStub) ListByFlowRun(context.Context, string) ([]*entities.TaskRunInfo, error) {
	s.listCalled = true
	return nil, nil
}

func TestFlowRunListDoesNotDiscloseOtherNamespaces(t *testing.T) {
	t.Parallel()
	tenantA := &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}}
	store := &flowRunStoreStub{page: &entities.Page[entities.FlowRunInfo]{
		Items: []*entities.FlowRunInfo{tenantA}, TotalCount: 1,
	}}
	handler := &FlowRunHandler{runStore: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/runs", handler.List)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-a/runs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var page entities.Page[entities.FlowRunInfo]
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

func TestFlowRunLifecycleUpdatePersistsJobMasterTimestamps(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Minute)
	store := &flowRunStoreStub{runs: map[string]*entities.FlowRunInfo{
		"run-a": {
			BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"},
			Status:     entities.RunPending,
		},
	}}
	handler := &FlowRunHandler{runStore: store}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{run_id}", handler.Update)

	body := `{"status":"COMPLETED","started_at":"2026-08-16T00:00:00Z","finished_at":"2026-08-16T00:03:00Z"}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/runs/run-a", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.updated == nil || store.updated.Status != entities.RunCompleted {
		t.Fatalf("updated run = %+v", store.updated)
	}
	if !store.updated.StartedAt.Equal(started) || !store.updated.FinishedAt.Equal(finished) {
		t.Fatalf("timestamps = %v / %v", store.updated.StartedAt, store.updated.FinishedAt)
	}
}

func TestFlowRunLifecycleRejectsInvertedTimestamps(t *testing.T) {
	t.Parallel()
	store := &flowRunStoreStub{runs: map[string]*entities.FlowRunInfo{
		"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
	}}
	handler := &FlowRunHandler{runStore: store}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{run_id}", handler.Update)

	body := `{"status":"FAILED","started_at":"2026-08-16T00:03:00Z","finished_at":"2026-08-16T00:00:00Z"}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/runs/run-a", strings.NewReader(body)))
	if response.Code != http.StatusBadRequest || store.updated != nil {
		t.Fatalf("status = %d, updated = %+v", response.Code, store.updated)
	}
}

func TestFlowRunTasksRequireRunNamespaceOwnership(t *testing.T) {
	t.Parallel()
	tasks := &taskStoreStub{}
	handler := &FlowRunHandler{
		runStore: &flowRunStoreStub{runs: map[string]*entities.FlowRunInfo{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
		}},
		taskStore: tasks,
		payloads:  taskpayload.NewDefaultTaskPayloadProvider(0),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{run_id}/node-runs", handler.ListTasks)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-b/runs/run-a/node-runs", nil))
	if response.Code != http.StatusNotFound || tasks.listCalled {
		t.Fatalf("status = %d, list called = %v", response.Code, tasks.listCalled)
	}
}

func TestUpdateTaskUsesOwnedRunFromPath(t *testing.T) {
	t.Parallel()
	tasks := &taskStoreStub{tasks: map[string]*entities.TaskRunInfo{}}
	handler := &FlowRunHandler{
		runStore: &flowRunStoreStub{runs: map[string]*entities.FlowRunInfo{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
		}},
		taskStore: tasks,
		payloads:  taskpayload.NewDefaultTaskPayloadProvider(0),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{run_id}/node-runs/{node_run_id}", handler.UpdateTask)

	body := `{"run_id":"run-b","namespace_id":"tenant-b","node_key":"node-a","status":"SUCCESS","attempt":1}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/runs/run-a/node-runs/node-run-a", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if tasks.updated == nil || tasks.updated.RunID != "run-a" || tasks.updated.Namespace != "tenant-a" || tasks.updated.NodeKey != "node-a" {
		t.Fatalf("updated task = %+v", tasks.updated)
	}
}

func TestUpdateNodeRunRejectsLegacyTransportAliases(t *testing.T) {
	t.Parallel()
	handler := &FlowRunHandler{
		runStore: &flowRunStoreStub{runs: map[string]*entities.FlowRunInfo{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
		}},
		taskStore: &taskStoreStub{tasks: map[string]*entities.TaskRunInfo{}},
		payloads:  taskpayload.NewDefaultTaskPayloadProvider(0),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{run_id}/node-runs/{node_run_id}", handler.UpdateTask)
	body := `{"agentflow_run_id":"run-a","node_id":"node-a","status":"SUCCESS"}`
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/runs/run-a/node-runs/node-run-a", strings.NewReader(body)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
