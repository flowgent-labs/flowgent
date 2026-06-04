package handler

import (
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"context"
	"github.com/flowgent-labs/flowgent/model/src"
)

// FlowRunStore is the subset of store.IStore needed by FlowRunHandler.
type FlowRunStore interface {
	GetFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	DeleteFlowRun(ctx context.Context, id string) error
	CancelFlowRun(ctx context.Context, id string) error
	ListTaskRunsByFlow(ctx context.Context, runID string) ([]model.TaskRun, error)
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
}

type FlowRunHandler struct {
	store  FlowRunStore
	logger *utils.Logger
}

func NewFlowRunHandler(s FlowRunStore, logger *utils.Logger) *FlowRunHandler {
	return &FlowRunHandler{store: s, logger: logger}
}

func (h *FlowRunHandler) List(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.ListFlowRuns(r.Context(), r.URL.Query().Get("agentflow_id"), 50)
	if err != nil { http.Error(w, err.Error(), 500); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (h *FlowRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.GetFlowRun(r.Context(), r.PathValue("id"))
	if err != nil || run == nil { http.Error(w, "not found", 404); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (h *FlowRunHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteFlowRun(r.Context(), r.PathValue("id")); err != nil { http.Error(w, "internal", 500); return }
	w.WriteHeader(200)
}

func (h *FlowRunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.store.CancelFlowRun(r.Context(), r.PathValue("id")); err != nil { http.Error(w, "internal", 500); return }
	w.WriteHeader(200)
}

func (h *FlowRunHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.store.ListTaskRunsByFlow(r.Context(), r.PathValue("id"))
	if err != nil { http.Error(w, err.Error(), 500); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (h *FlowRunHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.store.GetTaskRun(r.Context(), r.PathValue("task_id"))
	if err != nil || task == nil { http.Error(w, "not found", 404); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (h *FlowRunHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	var task model.TaskRun
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil { http.Error(w, "invalid body", 400); return }
	task.ID = r.PathValue("task_id")
	if err := h.store.UpdateTaskRun(r.Context(), &task); err != nil { h.logger.Error("update task", "error", err); http.Error(w, "internal", 500); return }
	w.WriteHeader(200)
}
