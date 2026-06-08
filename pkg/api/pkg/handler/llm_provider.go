package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

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
