package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agent"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentDefHandler manages dynamic agent CRUD via REST API.
type AgentDefHandler struct {
	store  agent.IAgentInfoStore
	logger *utils.Logger
}

// NewAgentDefHandler creates an agent CRUD handler.
func NewAgentDefHandler(s store.IStore, logger *utils.Logger) *AgentDefHandler {
	var agStore agent.IAgentInfoStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		agStore = agent.NewAgentPostgresStore(db)
	case *sql.DB:
		agStore = agent.NewAgentSQLiteStore(db)
	}
	return &AgentDefHandler{store: agStore, logger: logger}
}

// List returns all agent definitions for the given namespace.
func (h *AgentDefHandler) List(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	agents, err := h.store.Select(r.Context(), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := make([]*entities.AgentInfo, 0, len(agents.Items))
	for _, item := range agents.Items {
		if item != nil && item.Namespace == namespace {
			filtered = append(filtered, item)
		}
	}
	agents.Items = filtered
	agents.TotalCount = int64(len(filtered))
	agents.TotalPages = 1
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

// Create persists a new agent definition.
func (h *AgentDefHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var agent entities.AgentInfo
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if agent.Name == "" {
		http.Error(w, "agent name is required", http.StatusBadRequest)
		return
	}
	agent.ID = uuid.New().String()
	agent.Namespace = namespace
	if agent.Status == "" {
		agent.Status = "ACTIVE"
	}
	agent.CreatedAt = time.Now()
	agent.UpdatedAt = time.Now()
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
	if err != nil || agent == nil || agent.Namespace != r.PathValue("namespace") {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// Update modifies an existing agent definition.
func (h *AgentDefHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	namespace := r.PathValue("namespace")

	existing, err := h.store.Get(r.Context(), name)
	if err != nil || existing == nil || existing.Namespace != namespace {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}

	var updates entities.AgentInfo
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Merge: preserve existing values, apply non-zero updates
	if updates.Soul != "" {
		existing.Soul = updates.Soul
	}
	if updates.Instruction != "" {
		existing.Instruction = updates.Instruction
	}
	if updates.Model != "" {
		existing.Model = updates.Model
	}
	if updates.Temperature != nil {
		existing.Temperature = updates.Temperature
	}
	if updates.MaxTokens != 0 {
		existing.MaxTokens = updates.MaxTokens
	}
	if updates.OutputSchema != nil {
		existing.OutputSchema = updates.OutputSchema
	}
	if updates.Labels != nil {
		existing.Labels = updates.Labels
	}
	if updates.Description != "" {
		existing.Description = updates.Description
	}
	existing.UpdatedAt = time.Now()

	if err := h.store.Save(r.Context(), existing); err != nil {
		h.logger.Error("update agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

// Delete removes an agent definition.
func (h *AgentDefHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	existing, err := h.store.Get(r.Context(), name)
	if err != nil || existing == nil || existing.Namespace != r.PathValue("namespace") {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), name); err != nil {
		h.logger.Error("delete agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
