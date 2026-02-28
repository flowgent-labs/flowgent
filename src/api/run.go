package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/common/utils"
)

// RunHandler manages agentflow run endpoints (queries + lifecycle).
type RunHandler struct {
	store  RunStore
	logger *utils.Logger
}

// RunStore is the subset of store.Store needed by RunHandler.
type RunStore interface {
	GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	DeleteAgentFlowRun(ctx context.Context, id string) error
	CancelAgentFlowRun(ctx context.Context, id string) error
	GetTaskRunsByAgentFlowRun(ctx context.Context, agentFlowRunID string) ([]model.TaskRun, error)
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
}

// NewRunHandler creates a run management handler.
func NewRunHandler(s RunStore, logger *utils.Logger) *RunHandler {
	return &RunHandler{store: s, logger: logger}
}

// List returns agentflow runs filtered by optional agentflow_id query param.
func (h *RunHandler) List(w http.ResponseWriter, r *http.Request) {
	agentFlowID := r.URL.Query().Get("agentflow_id")
	runs, err := h.store.ListAgentFlowRuns(r.Context(), agentFlowID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// Get returns a single agentflow run by ID.
func (h *RunHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.store.GetAgentFlowRun(r.Context(), id)
	if err != nil || run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// Cancel stops a running agentflow run.
func (h *RunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.CancelAgentFlowRun(r.Context(), id); err != nil {
		h.logger.Error("cancel run", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "cancelled"})
}

// Delete removes a run record entirely.
func (h *RunHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteAgentFlowRun(r.Context(), id); err != nil {
		h.logger.Error("delete run", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListTasks returns all task runs for a given agentflow run.
func (h *RunHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	tasks, err := h.store.GetTaskRunsByAgentFlowRun(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// GetTask returns a single task run by ID.
func (h *RunHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	task, err := h.store.GetTaskRun(r.Context(), taskID)
	if err != nil || task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}
