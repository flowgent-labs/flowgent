package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/secretref"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/mcp"
)

const redactedSecret = "••••••••"

func publicMcp(m *entities.McpInfo) *entities.McpInfo {
	if m == nil {
		return nil
	}
	result := *m
	result.Headers = make(map[string]string, len(m.Headers))
	result.HeaderRefs = make(map[string]string, len(m.Headers))
	for key, value := range m.Headers {
		if secretref.IsSensitiveHeader(key) {
			result.Headers[key] = redactedSecret
			if secretref.TemplateIsReference(value) {
				result.HeaderRefs[key] = value
			}
			continue
		}
		result.Headers[key] = value
		result.HeaderRefs[key] = value
	}
	result.Env = make(map[string]string, len(m.Env))
	result.EnvRefs = make(map[string]string, len(m.Env))
	for key, value := range m.Env {
		result.Env[key] = redactedSecret
		if _, ok := secretref.EnvName(value); ok {
			result.EnvRefs[key] = value
		}
	}
	return &result
}

func normalizeMcpSecrets(m *entities.McpInfo, existing *entities.McpInfo) error {
	headers := m.Headers
	if m.HeaderRefs != nil {
		headers = m.HeaderRefs
	}
	if headers != nil {
		normalized := make(map[string]string, len(headers))
		for key, value := range headers {
			value = strings.TrimSpace(value)
			if value == redactedSecret && existing != nil {
				value = existing.Headers[key]
			}
			if secretref.IsSensitiveHeader(key) && !secretref.TemplateIsReference(value) {
				return fmt.Errorf("header %q must reference an injected environment secret", key)
			}
			normalized[key] = value
		}
		m.Headers = normalized
	}
	env := m.Env
	if m.EnvRefs != nil {
		env = m.EnvRefs
	}
	if env != nil {
		normalized := make(map[string]string, len(env))
		for key, value := range env {
			value = strings.TrimSpace(value)
			if value == redactedSecret && existing != nil {
				value = existing.Env[key]
			}
			name, ok := secretref.EnvName(value)
			if !ok {
				return fmt.Errorf("environment value %q must be an injected secret reference", key)
			}
			normalized[key] = "${" + name + "}"
		}
		m.Env = normalized
	}
	m.HeaderRefs = nil
	m.EnvRefs = nil
	return nil
}

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
	publicItems := make([]*entities.McpInfo, 0, len(items))
	for _, item := range items {
		if item.Namespace == r.PathValue("namespace") {
			publicItems = append(publicItems, publicMcp(item))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicItems)
}

func (h *McpHandler) Create(w http.ResponseWriter, r *http.Request) {
	var m entities.McpInfo
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	m.ID = uuid.New().String()
	m.Namespace = r.PathValue("namespace")
	if m.Type == "" {
		m.Type = "streamable-http"
	}
	if !strings.EqualFold(m.Type, "streamable-http") {
		http.Error(w, "only streamable-http MCP transport is supported", http.StatusBadRequest)
		return
	}
	if err := normalizeMcpSecrets(&m, nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	if err := h.store.Save(r.Context(), &m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(publicMcp(&m))
}

func (h *McpHandler) Get(w http.ResponseWriter, r *http.Request) {
	m, err := h.store.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		if isNotFoundError(err) {
			http.Error(w, "not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if m == nil || m.Namespace != r.PathValue("namespace") {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicMcp(m))
}

func (h *McpHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	existing, err := h.store.Get(r.Context(), name)
	if err != nil || existing == nil || existing.Namespace != r.PathValue("namespace") {
		http.Error(w, "not found", 404)
		return
	}

	var updates entities.McpInfo
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if updates.Type != "" && !strings.EqualFold(updates.Type, "streamable-http") {
		http.Error(w, "only streamable-http MCP transport is supported", http.StatusBadRequest)
		return
	}
	if err := normalizeMcpSecrets(&updates, existing); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	json.NewEncoder(w).Encode(publicMcp(existing))
}

func (h *McpHandler) Delete(w http.ResponseWriter, r *http.Request) {
	m, err := h.store.Get(r.Context(), r.PathValue("name"))
	if err != nil || m == nil || m.Namespace != r.PathValue("namespace") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}
