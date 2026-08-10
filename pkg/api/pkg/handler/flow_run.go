package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/task"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowRunHandler struct {
	runStore  flowrun.IFlowRunStore
	taskStore task.ITaskStore
	mqtt      MQTTPublisher
	logger    *utils.Logger
}

func NewFlowRunHandler(s store.IStore, mqtt MQTTPublisher, logger *utils.Logger) *FlowRunHandler {
	var runStore flowrun.IFlowRunStore
	var taskStore task.ITaskStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		runStore = flowrun.NewFlowRunPostgresStore(db)
		taskStore = task.NewTaskPostgresStore(db)
	case *sql.DB:
		runStore = flowrun.NewFlowRunSQLiteStore(db)
		taskStore = task.NewTaskSQLiteStore(db)
	}
	return &FlowRunHandler{runStore: runStore, taskStore: taskStore, mqtt: mqtt, logger: logger}
}

// Create inserts a new FlowRunInfo.
func (h *FlowRunHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var run entities.FlowRunInfo
	if err := json.NewDecoder(r.Body).Decode(&run); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if run.Namespace == "" {
		run.Namespace = namespace
	}
	if err := h.runStore.Create(r.Context(), &run); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	h.publishRunCreatedEvent(r.Context(), &run)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(run)
}

// publishRunCreatedEvent emits the `ctrl/run/created` lifecycle event on MQTT
// for a directly-created run (POST /runs — used by the Controller's
// createApplicationRun and by tests). Mirrors FlowDefHandler's event so both
// run-creation paths publish the identical topic/payload (docs §2.4 /
// VERIFICATION.md §4.2.4). Best-effort — a broker outage never fails the
// create.
func (h *FlowRunHandler) publishRunCreatedEvent(ctx context.Context, run *entities.FlowRunInfo) {
	if h.mqtt == nil {
		return
	}
	topic := fmt.Sprintf("flowgent/v1/%s/flows/%s/runs/%s/ctrl/run/created", run.Namespace, run.AgentFlowID, run.ID)
	payload, _ := json.Marshal(map[string]any{
		"action":       "created",
		"run_id":       run.ID,
		"agentflow_id": run.AgentFlowID,
		"namespace_id": run.Namespace,
		"namespace":    run.K8sNamespace,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		h.logger.Warn("mqtt run created event publish failed", "topic", topic, "error", err)
	}
}

// List returns runs with optional query-param filters.
func (h *FlowRunHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 {
		size = 50
	}
	pageReq := entities.PageRequest{Page: page, Size: size}

	runs, err := h.runStore.Select(r.Context(), pageReq)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	status := q.Get("status")
	namespace := q.Get("k8s_namespace")
	flowID := q.Get("agentflow_id")
	filtered := filterRuns(runs.Items, status, namespace, flowID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entities.Page[entities.FlowRunInfo]{Items: filtered, TotalCount: int64(len(filtered))})
}

func filterRuns(runs []*entities.FlowRunInfo, status, namespace, flowID string) []*entities.FlowRunInfo {
	if status == "" && namespace == "" && flowID == "" {
		return runs
	}
	var out []*entities.FlowRunInfo
	for _, r := range runs {
		if status != "" && string(r.Status) != status {
			continue
		}
		if namespace != "" && r.K8sNamespace != namespace {
			continue
		}
		if flowID != "" && r.AgentFlowID != flowID {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (h *FlowRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	run, err := h.runStore.Get(r.Context(), r.PathValue("id"))
	if err != nil || run == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// Update updates run status fields.
func (h *FlowRunHandler) Update(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	var req struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	run, err := h.runStore.Get(r.Context(), runID)
	if err != nil || run == nil {
		http.Error(w, "not found", 404)
		return
	}
	if req.Status != "" {
		run.Status = entities.RunStatus(req.Status)
	}
	if req.Error != "" {
		run.Error = req.Error
	}
	if err := h.runStore.Update(r.Context(), run); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}

func (h *FlowRunHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.runStore.Delete(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}

func (h *FlowRunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.runStore.Cancel(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}

func (h *FlowRunHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.taskStore.ListByFlowRun(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// CreateTask creates a new task run for a flow run.
func (h *FlowRunHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var task entities.TaskRunInfo
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	task.AgentFlowRunID = r.PathValue("id")
	if err := h.taskStore.CreateTaskRun(r.Context(), &task); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(task)
}

func (h *FlowRunHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.taskStore.Get(r.Context(), r.PathValue("task_id"))
	if err != nil || task == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (h *FlowRunHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	var task entities.TaskRunInfo
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	task.ID = r.PathValue("task_id")
	if err := h.taskStore.UpdateTaskRun(r.Context(), &task); err != nil {
		h.logger.Error("save task", "error", err)
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}
