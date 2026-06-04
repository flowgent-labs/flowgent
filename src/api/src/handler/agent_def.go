package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
)

// AgentDefHandler manages dynamic agent CRUD via REST API.
type AgentDefHandler struct {
	store  AgentStore
	logger *utils.Logger
}

// AgentStore is the subset of store.IStore needed by AgentDefHandler.
type AgentStore interface {
	SaveAgent(ctx context.Context, agent *model.AgentDef) error
	GetAgent(ctx context.Context, name string) (*model.AgentDef, error)
	ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error)
	DeleteAgent(ctx context.Context, name string) error
}

// NewAgentHandler creates an agent CRUD handler.
func NewAgentDefHandler(s AgentStore, logger *utils.Logger) *AgentDefHandler {
	return &AgentDefHandler{store: s, logger: logger}
}

// List returns all agent definitions for the given tenant.
func (h *AgentDefHandler) List(w http.ResponseWriter, r *http.Request) {
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
func (h *AgentDefHandler) Create(w http.ResponseWriter, r *http.Request) {
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
func (h *AgentDefHandler) Get(w http.ResponseWriter, r *http.Request) {
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
func (h *AgentDefHandler) Update(w http.ResponseWriter, r *http.Request) {
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
func (h *AgentDefHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.store.DeleteAgent(r.Context(), name); err != nil {
		h.logger.Error("delete agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
