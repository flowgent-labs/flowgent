package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/llmprovider"
)

// fixEnabled derives the Enabled field from Status for API responses.
// Enabled has db:"-" so it is never persisted; Status is the source of truth.
func fixEnabled(p *entities.LlmProviderInfo) *entities.LlmProviderInfo {
	if p != nil {
		p.Enabled = p.Status == "ACTIVE"
	}
	return p
}

// LlmProviderHandler serves DB-backed LLM provider definitions.
type LlmProviderHandler struct {
	store llmprovider.ILlmProviderStore
}

// NewLlmProviderHandler creates an LlmProviderHandler from an IStore.
func NewLlmProviderHandler(s storepkg.IStore) *LlmProviderHandler {
	var lpStore llmprovider.ILlmProviderStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		lpStore = llmprovider.NewLlmProviderPostgresStore(db)
	case *sql.DB:
		lpStore = llmprovider.NewLlmProviderSQLiteStore(db)
	}
	return &LlmProviderHandler{store: lpStore}
}

// List returns all LLM provider definitions for a tenant.
func (h *LlmProviderHandler) List(w http.ResponseWriter, r *http.Request) {
	page, err := h.store.Select(r.Context(), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := page.Items
	if items == nil {
		items = []*entities.LlmProviderInfo{}
	}
	for _, p := range items {
		fixEnabled(p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// Create adds a new LLM provider.
func (h *LlmProviderHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p entities.LlmProviderInfo
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	p.ID = uuid.New().String()
	p.TenantID = r.PathValue("tenant")
	if p.ApiKey == "" {
		if v, ok := p.Credentials["apikey"]; ok {
			if vs, ok := v.(string); ok {
				p.ApiKey = vs
			}
		}
	}
	if p.Status == "" {
		if p.Enabled {
			p.Status = "ACTIVE"
		} else {
			p.Status = "INACTIVE"
		}
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	if err := h.store.Save(r.Context(), &p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(p)
}

// Get returns a single LLM provider by ID.
func (h *LlmProviderHandler) Get(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if p == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fixEnabled(p))
}

// Update modifies an existing LLM provider.
func (h *LlmProviderHandler) Update(w http.ResponseWriter, r *http.Request) {
	var p entities.LlmProviderInfo
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	p.ID = r.PathValue("id")
	if p.ApiKey == "" {
		if v, ok := p.Credentials["apikey"]; ok {
			if vs, ok := v.(string); ok {
				p.ApiKey = vs
			}
		}
	}
	if p.Status == "" {
		if p.Enabled {
			p.Status = "ACTIVE"
		} else {
			p.Status = "INACTIVE"
		}
	}
	p.UpdatedAt = time.Now()
	if err := h.store.Save(r.Context(), &p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// Delete removes an LLM provider.
func (h *LlmProviderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}
