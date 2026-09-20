package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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

// Create adds a new knowledge entry.
func (h *KnowledgeHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")

	var entry entities.KnowledgeEntry
	if err := decodeStrictJSON(r, &entry); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if entry.Title == "" && entry.Content == "" {
		http.Error(w, "title or content is required", http.StatusBadRequest)
		return
	}
	if entry.Namespace != "" && entry.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}

	entry.ID = uuid.New().String()
	entry.Namespace = namespace
	entry.CreatedAt = time.Now()
	entry.UpdatedAt = time.Now()
	if entry.Status == "" {
		entry.Status = "ACTIVE"
	}
	if entry.Tags == nil {
		entry.Tags = []string{}
	}

	if err := h.store.Save(r.Context(), &entry); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry)
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

// Update modifies an existing knowledge entry.
func (h *KnowledgeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	namespace := r.PathValue("namespace")

	existing, err := h.store.Get(r.Context(), namespace, id)
	if err != nil || existing == nil {
		http.Error(w, "knowledge entry not found", http.StatusNotFound)
		return
	}

	var updates entities.KnowledgeEntry
	if err := decodeStrictJSON(r, &updates); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if updates.ID != "" && updates.ID != id {
		http.Error(w, "id mismatch", http.StatusBadRequest)
		return
	}
	if updates.Namespace != "" && updates.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if updates.Title == "" && updates.Content == "" {
		http.Error(w, "title or content is required", http.StatusBadRequest)
		return
	}
	updates.ID = id
	updates.Namespace = namespace
	updates.Status = existing.Status
	updates.CreatedAt = existing.CreatedAt
	updates.CreatedBy = existing.CreatedBy
	updates.UpdatedAt = time.Now()
	updates.UpdatedBy = existing.UpdatedBy
	updates.DelFlag = false
	if updates.Tags == nil {
		updates.Tags = []string{}
	}
	if updates.Metadata == nil {
		updates.Metadata = map[string]any{}
	}

	if err := h.store.Save(r.Context(), &updates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updates)
}

// Delete removes a knowledge entry by ID.
func (h *KnowledgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	namespace := r.PathValue("namespace")
	if _, err := h.store.Get(r.Context(), namespace, id); err != nil {
		http.Error(w, "knowledge entry not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), namespace, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Search performs a knowledge search with optional tag filtering.
func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	var req entities.KnowledgeSearchRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
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
