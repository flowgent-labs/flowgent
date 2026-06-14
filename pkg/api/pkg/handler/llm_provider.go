package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/llmprovider"
)

// LlmProviderHandler serves DB-backed LLM provider definitions.
type LlmProviderHandler struct {
	store llmprovider.ILlmProviderStore
}

// NewLlmProviderHandler creates an LlmProviderHandler from an IStore.
func NewLlmProviderHandler(s store.IStore) *LlmProviderHandler {
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
	page, err := h.store.Select(r.Context(), model.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := page.Items
	if items == nil {
		items = []*model.LlmProvider{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// Create adds a new LLM provider.
func (h *LlmProviderHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p model.LlmProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	p.ID = uuid.New().String()
	p.TenantID = r.PathValue("tenant")
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
	json.NewEncoder(w).Encode(p)
}

// Update modifies an existing LLM provider.
func (h *LlmProviderHandler) Update(w http.ResponseWriter, r *http.Request) {
	var p model.LlmProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	p.ID = r.PathValue("id")
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
