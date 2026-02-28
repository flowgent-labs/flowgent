package api

import (
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/store"
	"github.com/flowgent-labs/flowgent/src/common/utils"
)

// AgentFlowHandler manages agentflow HTTP endpoints.
type AgentFlowHandler struct {
	store      store.Store
	logger     *utils.Logger
	agentFlows map[string]*model.AgentFlowSpec
}

// NewAgentFlowHandler creates an agentflow HTTP handler.
func NewAgentFlowHandler(s store.Store, logger *utils.Logger, agentFlows []model.AgentFlowSpec, subAgentFlows map[string]model.AgentFlowSpec) *AgentFlowHandler {
	afMap := make(map[string]*model.AgentFlowSpec)
	for i := range agentFlows {
		afMap[agentFlows[i].ID] = &agentFlows[i]
	}
	for k := range subAgentFlows {
		sw := subAgentFlows[k]
		afMap[k] = &sw
	}
	return &AgentFlowHandler{store: s, logger: logger, agentFlows: afMap}
}

// ListDefinitions returns all saved agentflow definitions.
func (h *AgentFlowHandler) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := h.store.ListAgentFlowDefinitions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defs)
}

// ListRuns returns agentflow runs filtered by agentflow_id.
func (h *AgentFlowHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	agentFlowID := r.URL.Query().Get("agentflow_id")
	runs, err := h.store.ListAgentFlowRuns(r.Context(), agentFlowID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// GetRun returns a single agentflow run by ID.
func (h *AgentFlowHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.store.GetAgentFlowRun(r.Context(), id)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// GetTaskRuns returns task runs for a given agentflow run.
func (h *AgentFlowHandler) GetTaskRuns(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	tasks, err := h.store.GetTaskRunsByAgentFlowRun(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// Trigger starts a new agentflow run via API call.
func (h *AgentFlowHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentFlowID string            `json:"agentflow_id"`
		Vars        map[string]any    `json:"vars,omitempty"`
		Trigger     model.TriggerInfo `json:"trigger"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	spec := h.agentFlows[req.AgentFlowID]
	if spec == nil {
		http.Error(w, "agentflow not found", http.StatusNotFound)
		return
	}
	run := &model.AgentFlowRun{
		AgentFlowID: req.AgentFlowID,
		Version:     1,
		Status:      model.RunPending,
		Vars:        req.Vars,
		Trigger:     req.Trigger,
	}
	if err := h.store.CreateAgentFlowRun(r.Context(), run); err != nil {
		h.logger.Error("create agentflow run", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.logger.Info("agentflow triggered", "id", run.ID, "agentflow", req.AgentFlowID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"run_id": run.ID, "agentflow_id": req.AgentFlowID, "status": run.Status})
}

// AgentFlows returns the currently loaded agentflow specs.
func (h *AgentFlowHandler) AgentFlows() map[string]*model.AgentFlowSpec {
	return h.agentFlows
}

// Reload updates the internal agentflow map from new specs.
func (h *AgentFlowHandler) Reload(agentFlows []model.AgentFlowSpec, subAgentFlows map[string]model.AgentFlowSpec) {
	afMap := make(map[string]*model.AgentFlowSpec)
	for i := range agentFlows {
		afMap[agentFlows[i].ID] = &agentFlows[i]
	}
	for k := range subAgentFlows {
		sw := subAgentFlows[k]
		afMap[k] = &sw
	}
	h.agentFlows = afMap
}
