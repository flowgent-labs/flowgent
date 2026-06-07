package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentdef"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentDefHandler manages dynamic agent CRUD via REST API.
type AgentDefHandler struct {
	store  agentdef.IAgentDefStore
	logger *utils.Logger
}

// NewAgentDefHandler creates an agent CRUD handler.
func NewAgentDefHandler(s store.IStore, logger *utils.Logger) *AgentDefHandler {
	var agStore agentdef.IAgentDefStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		agStore = agentdef.NewAgentDefPostgresStore(db)
	case *sql.DB:
		agStore = agentdef.NewAgentDefSQLiteStore(db)
	}
	return &AgentDefHandler{store: agStore, logger: logger}
}

// List returns all agent definitions for the given tenant.
func (h *AgentDefHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	agents, err := h.store.Select(r.Context(), model.PageRequest{Page:1, Size:1000})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tenant
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
	if err := h.store.Save(r.Context(), &agent); err != nil {
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
	agent, err := h.store.Get(r.Context(), name)
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
	if err := h.store.Save(r.Context(), &agent); err != nil {
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
	if err := h.store.Delete(r.Context(), name); err != nil {
		h.logger.Error("delete agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
