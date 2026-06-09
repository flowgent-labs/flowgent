package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/taskplan"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowRunHandler struct {
	runStore  flowrun.IFlowRunStore
	taskStore taskplan.ITaskPlanStore
	mqtt      MQTTPublisher
	logger    *utils.Logger
}

func NewFlowRunHandler(s store.IStore, mqtt MQTTPublisher, logger *utils.Logger) *FlowRunHandler {
	var runStore flowrun.IFlowRunStore
	var taskStore taskplan.ITaskPlanStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		runStore = flowrun.NewFlowRunPostgresStore(db)
		taskStore = taskplan.NewTaskPlanPostgresStore(db)
	case *sql.DB:
		runStore = flowrun.NewFlowRunSQLiteStore(db)
		taskStore = taskplan.NewTaskPlanSQLiteStore(db)
	}
	return &FlowRunHandler{runStore: runStore, taskStore: taskStore, mqtt: mqtt, logger: logger}
}

// Create inserts a new AgentFlowRun.
func (h *FlowRunHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	var run model.AgentFlowRun
	if err := json.NewDecoder(r.Body).Decode(&run); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if run.TenantID == "" {
		run.TenantID = tenant
	}
	if err := h.runStore.Create(r.Context(), &run); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(run)
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
	pageReq := model.PageRequest{Page: page, Size: size}

	runs, err := h.runStore.Select(r.Context(), pageReq)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	status := q.Get("status")
	namespace := q.Get("namespace")
	flowID := q.Get("agentflow_id")
	filtered := filterRuns(runs.Items, status, namespace, flowID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model.Page[model.AgentFlowRun]{Items: filtered, TotalCount: int64(len(filtered))})
}

func filterRuns(runs []*model.AgentFlowRun, status, namespace, flowID string) []*model.AgentFlowRun {
	if status == "" && namespace == "" && flowID == "" {
		return runs
	}
	var out []*model.AgentFlowRun
	for _, r := range runs {
		if status != "" && string(r.Status) != status {
			continue
		}
		if namespace != "" && r.Namespace != namespace {
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
		run.Status = model.RunStatus(req.Status)
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
	var task model.TaskRun
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
	var task model.TaskRun
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	task.ID = r.PathValue("task_id")
	if err := h.taskStore.Save(r.Context(), &task); err != nil {
		h.logger.Error("save task", "error", err)
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}
