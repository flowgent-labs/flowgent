package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/agent"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentDefHandler manages dynamic agent CRUD via REST API.
type AgentDefHandler struct {
	store  agent.IAgentInfoStore
	logger *utils.Logger
}

// NewAgentDefHandler creates an agent CRUD handler.
func NewAgentDefHandler(s storage.IStorage, logger *utils.Logger) *AgentDefHandler {
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
	agents, err := h.store.List(r.Context(), namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

// Create persists a new agent definition.
func (h *AgentDefHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var agent entities.AgentInfo
	if err := decodeStrictJSON(r, &agent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !resourceNamePattern.MatchString(agent.Name) {
		http.Error(w, "name may contain only letters, digits, hyphens, and underscores", http.StatusBadRequest)
		return
	}
	if agent.Namespace != "" && agent.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if existing, err := h.store.Get(r.Context(), namespace, agent.Name); err == nil && existing != nil {
		http.Error(w, "agent already exists", http.StatusConflict)
		return
	}
	agent.ID = uuid.New().String()
	agent.Namespace = namespace
	agent.Revision = 1
	agent.Version = 1
	if agent.Status == "" {
		agent.Status = "ACTIVE"
	}
	agent.CreatedAt = time.Now()
	agent.UpdatedAt = time.Now()
	agent.CreatedBy = authenticatedUserID(r.Context())
	agent.UpdatedBy = agent.CreatedBy
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
	agent, err := h.store.Get(r.Context(), r.PathValue("namespace"), name)
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
	namespace := r.PathValue("namespace")

	existing, err := h.store.Get(r.Context(), namespace, name)
	if err != nil || existing == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}

	var updates entities.AgentInfo
	if err := decodeStrictJSON(r, &updates); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if updates.Namespace != "" && updates.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if updates.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}
	if updates.Name == "" {
		updates.Name = name
	}
	if !resourceNamePattern.MatchString(updates.Name) {
		http.Error(w, "name may contain only letters, digits, hyphens, and underscores", http.StatusBadRequest)
		return
	}
	if updates.Name != name {
		if duplicate, err := h.store.Get(r.Context(), namespace, updates.Name); err == nil && duplicate != nil {
			http.Error(w, "agent already exists", http.StatusConflict)
			return
		}
	}
	updates.ID = existing.ID
	updates.Namespace = namespace
	updates.Revision = existing.Revision + 1
	updates.Version = updates.Revision
	updates.Status = existing.Status
	updates.CreatedAt = existing.CreatedAt
	updates.CreatedBy = existing.CreatedBy
	updates.UpdatedAt = time.Now()
	updates.UpdatedBy = authenticatedUserID(r.Context())

	if err := h.store.Save(r.Context(), &updates); err != nil {
		h.logger.Error("update agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updates)
}

// Delete removes an agent definition.
func (h *AgentDefHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	namespace := r.PathValue("namespace")
	existing, err := h.store.Get(r.Context(), namespace, name)
	if err != nil || existing == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), namespace, name); err != nil {
		h.logger.Error("delete agent", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
