package handler

import (
	"database/sql"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/knowledge"
)

// KnowledgeHandler serves DB-backed knowledge entries via REST API.
type KnowledgeHandler struct {
	store knowledge.IKnowledgeStore
}

// NewKnowledgeHandler creates a KnowledgeHandler from an IStorage.
func NewKnowledgeHandler(s storage.IStorage) *KnowledgeHandler {
	var kStore knowledge.IKnowledgeStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		kStore = knowledge.NewKnowledgePostgresStore(db)
	case *sql.DB:
		kStore = knowledge.NewKnowledgeSQLiteStore(db)
	}
	return &KnowledgeHandler{store: kStore}
}

// List returns one stable paginated representation after applying all filters
// inside the namespace-scoped database query.
func (h *KnowledgeHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	size, _ := strconv.Atoi(query.Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}

	tags := make([]string, 0)
	for _, tag := range strings.Split(query.Get("tags"), ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	result, err := h.store.List(r.Context(), knowledge.ListFilter{
		Namespace: r.PathValue("namespace"),
		Scope:     query.Get("scope"),
		Tags:      tags,
		Page:      entities.PageRequest{Page: page, Size: size},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result.Items == nil {
		result.Items = []*entities.KnowledgeEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Get returns a single knowledge entry by ID.
func (h *KnowledgeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, err := h.store.Get(r.Context(), r.PathValue("namespace"), id)
	if err != nil {
		http.Error(w, "knowledge entry not found", http.StatusNotFound)
		return
	}
	if entry == nil {
		http.Error(w, "knowledge entry not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// Search performs a knowledge search with optional tag filtering.
func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	var req entities.KnowledgeSearchRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Query) == "" && len(req.Embedding) == 0 {
		http.Error(w, "query or embedding is required", http.StatusBadRequest)
		return
	}

	results, err := h.store.Search(r.Context(), r.PathValue("namespace"), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []*entities.KnowledgeEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// CreateCandidate freezes a run-summary publication request and creates the
// single unified approval record that governs its eventual publication.
func (h *KnowledgeHandler) CreateCandidate(w http.ResponseWriter, r *http.Request) {
	var candidate entities.KnowledgeCandidate
	if err := decodeStrictJSON(r, &candidate); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	namespace := r.PathValue("namespace")
	if candidate.Namespace != "" && candidate.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	candidate.Namespace = namespace
	candidate.CreatedBy = authenticatedUserID(r.Context())
	approval, err := h.store.CreateCandidate(r.Context(), &candidate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"candidate": candidate, "approval": approval})
}

// ListTags returns all distinct tags from knowledge entries.
func (h *KnowledgeHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context(), r.PathValue("namespace"), r.URL.Query().Get("scope"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}
