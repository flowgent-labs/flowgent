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
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/mcp"
)

func publicMcp(m *entities.McpInfo) *entities.McpInfo {
	if m == nil {
		return nil
	}
	result := *m
	result.Headers = nil
	result.HeaderRefs = make(map[string]string, len(m.Headers))
	for key, value := range m.Headers {
		result.HeaderRefs[key] = value
	}
	result.Env = nil
	result.EnvRefs = make(map[string]string, len(m.Env))
	for key, value := range m.Env {
		if _, ok := secretref.EnvName(value); ok {
			result.EnvRefs[key] = value
		}
	}
	return &result
}

func normalizeMcpSecrets(m *entities.McpInfo) error {
	headers := m.HeaderRefs
	if headers != nil {
		normalized := make(map[string]string, len(headers))
		for key, value := range headers {
			value = strings.TrimSpace(value)
			if secretref.IsSensitiveHeader(key) && !secretref.TemplateIsReference(value) {
				return fmt.Errorf("header %q must reference an injected environment secret", key)
			}
			normalized[key] = value
		}
		m.Headers = normalized
	}
	env := m.EnvRefs
	if env != nil {
		normalized := make(map[string]string, len(env))
		for key, value := range env {
			value = strings.TrimSpace(value)
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

// NewMcpHandler creates an McpHandler from an IStorage.
func NewMcpHandler(s storage.IStorage) *McpHandler {
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
	page, err := h.store.List(r.Context(), r.PathValue("namespace"), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	publicItems := make([]*entities.McpInfo, 0, len(page.Items))
	for _, item := range page.Items {
		publicItems = append(publicItems, publicMcp(item))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicItems)
}

func (h *McpHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var m entities.McpInfo
	if err := decodeStrictJSON(r, &m); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if m.Namespace != "" && m.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	m.ID = uuid.New().String()
	m.Namespace = namespace
	if !strings.EqualFold(m.Type, "streamable-http") {
		http.Error(w, "only streamable-http MCP transport is supported", http.StatusBadRequest)
		return
	}
	if err := normalizeMcpSecrets(&m); err != nil {
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
	m, err := h.store.Get(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		if isNotFoundError(err) {
			http.Error(w, "not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if m == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicMcp(m))
}

func (h *McpHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	namespace := r.PathValue("namespace")
	existing, err := h.store.Get(r.Context(), namespace, name)
	if err != nil || existing == nil {
		http.Error(w, "not found", 404)
		return
	}

	var updates entities.McpInfo
	if err := decodeStrictJSON(r, &updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if updates.Name != "" && updates.Name != name {
		http.Error(w, "name mismatch", http.StatusBadRequest)
		return
	}
	if updates.Namespace != "" && updates.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(updates.Type, "streamable-http") {
		http.Error(w, "only streamable-http MCP transport is supported", http.StatusBadRequest)
		return
	}
	if err := normalizeMcpSecrets(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	updates.ID = existing.ID
	updates.Name = name
	updates.Namespace = namespace
	updates.Status = existing.Status
	updates.CreatedAt = existing.CreatedAt
	updates.CreatedBy = existing.CreatedBy
	updates.UpdatedAt = time.Now()
	updates.UpdatedBy = existing.UpdatedBy
	updates.DelFlag = false

	if err := h.store.Save(r.Context(), &updates); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicMcp(&updates))
}

func (h *McpHandler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	m, err := h.store.Get(r.Context(), namespace, r.PathValue("name"))
	if err != nil || m == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), namespace, r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}
