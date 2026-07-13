package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/mcp"
)

// McpHandler serves DB-backed MCP server definitions.
type McpHandler struct {
	store mcp.IMCPStore
}

// NewMcpHandler creates an McpHandler from an IStore.
func NewMcpHandler(s store.IStore) *McpHandler {
	var mcpStore mcp.IMCPStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		mcpStore = mcp.NewMCPPostgresStore(db)
	case *sql.DB:
		mcpStore = mcp.NewMCPSQLiteStore(db)
	}
	return &McpHandler{store: mcpStore}
}

func (h *McpHandler) List(w http.ResponseWriter, r *http.Request) {
	page, err := h.store.Select(r.Context(), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := page.Items
	if items == nil {
		items = []*entities.McpInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *McpHandler) Create(w http.ResponseWriter, r *http.Request) {
	var m entities.McpInfo
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	m.ID = uuid.New().String()
	m.TenantID = r.PathValue("tenant")
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	if err := h.store.Save(r.Context(), &m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(m)
}

func (h *McpHandler) Get(w http.ResponseWriter, r *http.Request) {
	m, err := h.store.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if m == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

func (h *McpHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	existing, err := h.store.Get(r.Context(), name)
	if err != nil || existing == nil {
		http.Error(w, "not found", 404)
		return
	}

	var updates entities.McpInfo
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	if updates.Type != "" {
		existing.Type = updates.Type
	}
	if updates.URL != "" {
		existing.URL = updates.URL
	}
	if updates.Headers != nil {
		existing.Headers = updates.Headers
	}
	if updates.Command != nil {
		existing.Command = updates.Command
	}
	if updates.Args != nil {
		existing.Args = updates.Args
	}
	if updates.Env != nil {
		existing.Env = updates.Env
	}
	// Enabled is a bool — use a pointer or check if the JSON explicitly set it.
	// For now, always apply the value from the request body.
	existing.Enabled = updates.Enabled
	existing.UpdatedAt = time.Now()

	if err := h.store.Save(r.Context(), existing); err != nil {
		http.Error(w, "Save err: name='"+name+"' existing.Name='"+existing.Name+"' id='"+existing.ID+"' -> "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

func (h *McpHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}
