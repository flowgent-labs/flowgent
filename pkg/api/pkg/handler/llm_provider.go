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
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/llmprovider"
)

var supportedLlmTypes = map[string]struct{}{
	"openai": {}, "anthropic": {}, "gemini": {},
}

// publicLlmProvider returns the browser-safe resource projection. The API key
// is persisted only as an env:// reference and is never serialized back to a
// client. api_key_env is safe configuration metadata, not a credential value.
func publicLlmProvider(p *entities.LlmProviderInfo) *entities.LlmProviderInfo {
	if p == nil {
		return nil
	}
	result := *p
	result.Enabled = result.Status == "ACTIVE"
	result.KeyConfigured = strings.TrimSpace(result.ApiKey) != "" || strings.TrimSpace(result.CredentialRef) != ""
	if envName, ok := secretref.EnvName(result.ApiKey); ok {
		result.ApiKeyEnv = envName
	}
	result.ApiKey = ""
	result.EnvRefs = make(map[string]string, len(result.Env))
	for key, value := range result.Env {
		if _, ok := secretref.EnvName(value); ok {
			result.EnvRefs[key] = value
		}
	}
	result.Env = nil
	if result.Name == "" {
		result.Name = result.Provider
	}
	if result.Type == "" {
		result.Type = legacyLlmType(result.Provider)
	}
	return &result
}

func normalizeLlmSecret(p *entities.LlmProviderInfo) error {
	envName := strings.TrimSpace(p.ApiKeyEnv)
	if envName != "" {
		normalized, err := secretref.Normalize("${" + envName + "}")
		if err != nil {
			return fmt.Errorf("api_key_env: %w", err)
		}
		p.ApiKey = normalized
	}
	p.ApiKeyEnv = ""
	if p.EnvRefs != nil {
		env := make(map[string]string, len(p.EnvRefs))
		for key, value := range p.EnvRefs {
			name, ok := secretref.EnvName(strings.TrimSpace(value))
			if !ok {
				return fmt.Errorf("environment value %q must be an injected secret reference", key)
			}
			env[key] = "${" + name + "}"
		}
		p.Env = env
	}
	p.EnvRefs = nil
	return nil
}

func normalizeLlmIdentity(p *entities.LlmProviderInfo) error {
	p.NormalizeAliases()
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = strings.TrimSpace(p.Provider)
	}
	if !resourceNamePattern.MatchString(p.Name) {
		return fmt.Errorf("name may contain only letters, digits, hyphens, and underscores")
	}
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	if p.Type == "" {
		p.Type = legacyLlmType(p.Provider)
	}
	if _, ok := supportedLlmTypes[p.Type]; !ok {
		return fmt.Errorf("type must be one of openai, anthropic, or gemini")
	}
	// The runtime has historically used Provider as a lookup alias. Preserve
	// that contract while storing the protocol separately in Type.
	p.Provider = p.Name
	p.Endpoint = p.BaseURI
	if strings.TrimSpace(p.BaseURI) == "" {
		return fmt.Errorf("base_uri is required")
	}
	return nil
}

func legacyLlmType(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "gemini":
		return strings.ToLower(strings.TrimSpace(provider))
	default:
		return "openai"
	}
}

// LlmProviderHandler serves DB-backed LLM provider definitions.
type LlmProviderHandler struct {
	store llmprovider.ILlmProviderStore
}

// NewLlmProviderHandler creates an LlmProviderHandler from an IStorage.
func NewLlmProviderHandler(s storage.IStorage) *LlmProviderHandler {
	var lpStore llmprovider.ILlmProviderStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		lpStore = llmprovider.NewLlmProviderPostgresStore(db)
	case *sql.DB:
		lpStore = llmprovider.NewLlmProviderSQLiteStore(db)
	}
	return &LlmProviderHandler{store: lpStore}
}

// List returns all LLM provider definitions for a namespace.
func (h *LlmProviderHandler) List(w http.ResponseWriter, r *http.Request) {
	page, err := h.store.List(r.Context(), r.PathValue("namespace"), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	publicItems := make([]*entities.LlmProviderInfo, 0, len(page.Items))
	for _, p := range page.Items {
		publicItems = append(publicItems, publicLlmProvider(p))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicItems)
}

// Create adds a new LLM provider.
func (h *LlmProviderHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var p entities.LlmProviderInfo
	if err := decodeStrictJSON(r, &p); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if p.Namespace != "" && p.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if err := normalizeLlmIdentity(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if existing, err := h.store.List(r.Context(), namespace, entities.PageRequest{Page: 1, Size: 1000}); err == nil {
		for _, item := range existing.Items {
			if strings.EqualFold(item.Name, p.Name) || (item.Name == "" && strings.EqualFold(item.Provider, p.Name)) {
				http.Error(w, "LLM provider name already exists", http.StatusConflict)
				return
			}
		}
	}
	p.ID = uuid.New().String()
	p.Namespace = namespace
	if err := normalizeLlmSecret(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
	p.CreatedBy = authenticatedUserID(r.Context())
	p.UpdatedBy = p.CreatedBy
	if err := h.store.Save(r.Context(), &p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(publicLlmProvider(&p))
}

// Get returns a single LLM provider by ID.
func (h *LlmProviderHandler) Get(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.Context(), r.PathValue("namespace"), r.PathValue("id"))
	if err != nil {
		if isNotFoundError(err) {
			http.Error(w, "not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if p == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicLlmProvider(p))
}

// Update modifies an existing LLM provider.
func (h *LlmProviderHandler) Update(w http.ResponseWriter, r *http.Request) {
	namespace, id := r.PathValue("namespace"), r.PathValue("id")
	existing, err := h.store.Get(r.Context(), namespace, id)
	if err != nil || existing == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var updates entities.LlmProviderInfo
	if err := decodeStrictJSON(r, &updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	secretSupplied := strings.TrimSpace(updates.ApiKeyEnv) != ""
	if err := normalizeLlmSecret(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !secretSupplied {
		updates.ApiKey = existing.ApiKey
	}
	if updates.ID != "" && updates.ID != id {
		http.Error(w, "id mismatch", http.StatusBadRequest)
		return
	}
	if updates.Namespace != "" && updates.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if err := normalizeLlmIdentity(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if existingItems, err := h.store.List(r.Context(), namespace, entities.PageRequest{Page: 1, Size: 1000}); err == nil {
		for _, item := range existingItems.Items {
			if item.ID != id && (strings.EqualFold(item.Name, updates.Name) || (item.Name == "" && strings.EqualFold(item.Provider, updates.Name))) {
				http.Error(w, "LLM provider name already exists", http.StatusConflict)
				return
			}
		}
	}
	updates.ID = id
	updates.Namespace = namespace
	updates.CreatedAt = existing.CreatedAt
	updates.CreatedBy = existing.CreatedBy
	updates.RowVersion = existing.RowVersion
	if updates.Status == "" {
		if updates.Enabled {
			updates.Status = "ACTIVE"
		} else {
			updates.Status = existing.Status
		}
	}
	updates.UpdatedAt = time.Now()
	updates.UpdatedBy = authenticatedUserID(r.Context())
	if err := h.store.Save(r.Context(), &updates); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(publicLlmProvider(&updates))
}

// Delete removes an LLM provider.
func (h *LlmProviderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace, id := r.PathValue("namespace"), r.PathValue("id")
	p, err := h.store.Get(r.Context(), namespace, id)
	if err != nil || p == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), namespace, id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}
