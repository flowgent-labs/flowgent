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
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/knowledge"
)

// KnowledgeHandler serves DB-backed knowledge entries via REST API.
type KnowledgeHandler struct {
	store knowledge.IKnowledgeStore
}

// NewKnowledgeHandler creates a KnowledgeHandler from an IStore.
func NewKnowledgeHandler(s store.IStore) *KnowledgeHandler {
	var kStore knowledge.IKnowledgeStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		kStore = knowledge.NewKnowledgePostgresStore(db)
	case *sql.DB:
		kStore = knowledge.NewKnowledgeSQLiteStore(db)
	}
	return &KnowledgeHandler{store: kStore}
}

// List returns all knowledge entries for the given namespace, with optional
// tag filtering and pagination.
func (h *KnowledgeHandler) List(w http.ResponseWriter, r *http.Request) {
	tagsParam := r.URL.Query().Get("tags")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}

	if tagsParam != "" {
		tags := strings.Split(tagsParam, ",")
		results, err := h.store.SearchByTags(r.Context(), tags)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if results == nil {
			results = []*entities.KnowledgeEntry{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
		return
	}

	result, err := h.store.Select(r.Context(), entities.PageRequest{Page: page, Size: size})
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
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if entry.Title == "" && entry.Content == "" {
		http.Error(w, "title or content is required", http.StatusBadRequest)
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
	entry, err := h.store.Get(r.Context(), id)
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

	existing, err := h.store.Get(r.Context(), id)
	if err != nil || existing == nil {
		http.Error(w, "knowledge entry not found", http.StatusNotFound)
		return
	}

	var updates entities.KnowledgeEntry
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Merge: preserve existing values, apply non-zero updates
	if updates.Title != "" {
		existing.Title = updates.Title
	}
	if updates.Content != "" {
		existing.Content = updates.Content
	}
	if updates.ContentType != "" {
		existing.ContentType = updates.ContentType
	}
	if updates.Source != "" {
		existing.Source = updates.Source
	}
	if updates.SourceRef != "" {
		existing.SourceRef = updates.SourceRef
	}
	if updates.Tags != nil {
		existing.Tags = updates.Tags
	}
	if updates.Metadata != nil {
		existing.Metadata = updates.Metadata
	}
	existing.Namespace = namespace
	existing.UpdatedAt = time.Now()

	if err := h.store.Save(r.Context(), existing); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

// Delete removes a knowledge entry by ID.
func (h *KnowledgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Search performs a knowledge search with optional tag filtering.
func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	var req entities.KnowledgeSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	results, err := h.store.Search(r.Context(), req)
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
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}
