package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/common/utils"
)

// AgentHandler manages dynamic agent CRUD via REST API.
type AgentHandler struct {
	store  AgentStore
	logger *utils.Logger
}

// AgentStore is the subset of store.Store needed by AgentHandler.
type AgentStore interface {
	SaveAgent(ctx context.Context, agent *model.AgentDef) error
	GetAgent(ctx context.Context, name string) (*model.AgentDef, error)
	ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error)
	DeleteAgent(ctx context.Context, name string) error
}

// NewAgentHandler creates an agent CRUD handler.
func NewAgentHandler(s AgentStore, logger *utils.Logger) *AgentHandler {
	return &AgentHandler{store: s, logger: logger}
}

// List returns all agent definitions for the given tenant.
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	agents, err := h.store.ListAgents(r.Context(), tenant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

// Create persists a new agent definition.
func (h *AgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	var agent model.AgentDef
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if agent.Name == "" {
		http.Error(w, "agent name is required", http.StatusBadRequest)
		return
	}
	agent.TenantID = tenant
	if err := h.store.SaveAgent(r.Context(), &agent); err != nil {
		h.logger.Error("save agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(agent)
}

// Get returns a single agent definition by name.
func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	agent, err := h.store.GetAgent(r.Context(), name)
	if err != nil || agent == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// Update modifies an existing agent definition.
func (h *AgentHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tenant := r.PathValue("tenant")
	var agent model.AgentDef
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	agent.Name = name
	agent.TenantID = tenant
	if err := h.store.SaveAgent(r.Context(), &agent); err != nil {
		h.logger.Error("update agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// Delete removes an agent definition.
func (h *AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.store.DeleteAgent(r.Context(), name); err != nil {
		h.logger.Error("delete agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
