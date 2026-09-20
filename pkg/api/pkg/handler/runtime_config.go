package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flow"
	runtimeconfig "github.com/flowgent-labs/flowgent/storage/pkg/runtimeconfig"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxRuntimeConfigBodyBytes = 1 << 20
	maxRuntimeConfigEntries   = 128
	maxRuntimeConfigValueSize = 64 << 10
)

var runtimeEnvironmentKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// RuntimeConfigHandler owns inheritance, validation, redaction, and encryption
// for namespace/Flow runtime settings. The repository stores only local layers.
type RuntimeConfigHandler struct {
	repo      runtimeconfig.IRepository
	flows     flow.IFlowInfoStore
	secretBox secretbox.ISecretCipher
}

func NewRuntimeConfigHandler(s storage.IStorage, encryption config.NotifierSecretEncryptionConfig) (*RuntimeConfigHandler, error) {
	repo, err := runtimeconfig.NewRepository(s)
	if err != nil {
		return nil, err
	}
	var flows flow.IFlowInfoStore
	switch db := s.DB().(type) {
	case *sql.DB:
		flows = flow.NewFlowSQLiteStore(db)
	case *pgxpool.Pool:
		flows = flow.NewFlowPostgresStore(db)
	default:
		return nil, fmt.Errorf("runtime configuration: unsupported store %T", s.DB())
	}
	cipher, err := newDynamicSecretCipher(encryption)
	if err != nil {
		return nil, err
	}
	return &RuntimeConfigHandler{repo: repo, flows: flows, secretBox: cipher}, nil
}

func (h *RuntimeConfigHandler) GetNamespace(w http.ResponseWriter, r *http.Request) {
	h.get(w, r, entities.RuntimeConfigScopeNamespace, r.PathValue("namespace"))
}

func (h *RuntimeConfigHandler) UpdateNamespace(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, entities.RuntimeConfigScopeNamespace, r.PathValue("namespace"))
}

func (h *RuntimeConfigHandler) UpdateNamespaceEnvironment(w http.ResponseWriter, r *http.Request) {
	h.updateEnvironment(w, r, entities.RuntimeConfigScopeNamespace, r.PathValue("namespace"))
}

func (h *RuntimeConfigHandler) UpdateNamespaceSecrets(w http.ResponseWriter, r *http.Request) {
	h.updateSecrets(w, r, entities.RuntimeConfigScopeNamespace, r.PathValue("namespace"))
}

func (h *RuntimeConfigHandler) GetFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlow(w, r) {
		return
	}
	h.get(w, r, entities.RuntimeConfigScopeFlow, r.PathValue("flow_id"))
}

func (h *RuntimeConfigHandler) UpdateFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlow(w, r) {
		return
	}
	h.update(w, r, entities.RuntimeConfigScopeFlow, r.PathValue("flow_id"))
}

func (h *RuntimeConfigHandler) UpdateFlowEnvironment(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlow(w, r) {
		return
	}
	h.updateEnvironment(w, r, entities.RuntimeConfigScopeFlow, r.PathValue("flow_id"))
}

func (h *RuntimeConfigHandler) UpdateFlowSecrets(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlow(w, r) {
		return
	}
	h.updateSecrets(w, r, entities.RuntimeConfigScopeFlow, r.PathValue("flow_id"))
}

// ResolveFlow is a workload-only endpoint. Authorization grants it solely to
// the controller, which materializes the result into the Flow runtime boundary.
func (h *RuntimeConfigHandler) ResolveFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlow(w, r) {
		return
	}
	namespace, flowID := r.PathValue("namespace"), r.PathValue("flow_id")
	ns, err := h.load(r.Context(), namespace, entities.RuntimeConfigScopeNamespace, namespace)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	local, err := h.load(r.Context(), namespace, entities.RuntimeConfigScopeFlow, flowID)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	nsSecrets, err := h.resolveSecrets(r.Context(), ns)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	localSecrets, err := h.resolveSecrets(r.Context(), local)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	environment, secrets := mergeResolved(ns.Environment, nsSecrets, local.Environment, localSecrets)
	writeRuntimeJSON(w, http.StatusOK, entities.ResolvedRuntimeConfig{Environment: environment, Secrets: secrets})
}

func (h *RuntimeConfigHandler) get(w http.ResponseWriter, r *http.Request, scopeType, scopeID string) {
	namespace := r.PathValue("namespace")
	local, err := h.load(r.Context(), namespace, scopeType, scopeID)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	inherited := emptyRuntimeConfiguration(namespace, entities.RuntimeConfigScopeNamespace, namespace)
	if scopeType == entities.RuntimeConfigScopeFlow {
		inherited, err = h.load(r.Context(), namespace, entities.RuntimeConfigScopeNamespace, namespace)
		if err != nil {
			h.writeRuntimeError(w, err)
			return
		}
	}
	writeRuntimeJSON(w, http.StatusOK, runtimeConfigView(local, inherited, scopeType == entities.RuntimeConfigScopeFlow))
}

func (h *RuntimeConfigHandler) update(w http.ResponseWriter, r *http.Request, scopeType, scopeID string) {
	var update entities.RuntimeConfigUpdate
	if !decodeRuntimeConfigBody(w, r, &update) {
		return
	}
	h.applyUpdate(w, r, scopeType, scopeID, update)
}

func (h *RuntimeConfigHandler) updateEnvironment(w http.ResponseWriter, r *http.Request, scopeType, scopeID string) {
	var body struct {
		Environment map[string]string `json:"environment"`
	}
	if !decodeRuntimeConfigBody(w, r, &body) {
		return
	}
	h.applyUpdate(w, r, scopeType, scopeID, entities.RuntimeConfigUpdate{Environment: body.Environment})
}

func (h *RuntimeConfigHandler) updateSecrets(w http.ResponseWriter, r *http.Request, scopeType, scopeID string) {
	var body struct {
		Secrets         map[string]string `json:"secrets"`
		ClearSecretKeys []string          `json:"clear_secret_keys"`
	}
	if !decodeRuntimeConfigBody(w, r, &body) {
		return
	}
	item, err := h.load(r.Context(), r.PathValue("namespace"), scopeType, scopeID)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	h.applyUpdate(w, r, scopeType, scopeID, entities.RuntimeConfigUpdate{
		Environment: cloneStrings(item.Environment), Secrets: body.Secrets, ClearSecretKeys: body.ClearSecretKeys,
	})
}

func (h *RuntimeConfigHandler) applyUpdate(w http.ResponseWriter, r *http.Request, scopeType, scopeID string, update entities.RuntimeConfigUpdate) {
	if err := validateRuntimeConfigUpdate(&update); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	namespace := r.PathValue("namespace")
	item, err := h.load(r.Context(), namespace, scopeType, scopeID)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	secrets, err := h.resolveSecrets(r.Context(), item)
	if err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	for key, value := range update.Secrets {
		secrets[key] = value
	}
	for _, key := range update.ClearSecretKeys {
		delete(secrets, key)
	}
	item.Environment = cloneStrings(update.Environment)
	item.ConfiguredSecretKeys = sortedStringKeys(secrets)
	if len(secrets) == 0 {
		item.SealedSecrets = nil
	} else {
		item.SealedSecrets, err = secretbox.SealJSON(r.Context(), h.secretBox, secrets, runtimeConfigAAD(item))
		if err != nil {
			h.writeRuntimeError(w, err)
			return
		}
	}
	actor := authenticatedUserID(r.Context())
	if item.ID == "" {
		item.ID = uuid.NewString()
		item.MarkCreated(actor)
	} else {
		item.MarkUpdated(actor)
	}
	if err := h.repo.Upsert(r.Context(), item); err != nil {
		h.writeRuntimeError(w, err)
		return
	}
	h.get(w, r, scopeType, scopeID)
}

func decodeRuntimeConfigBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRuntimeConfigBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid runtime configuration", http.StatusBadRequest)
		return false
	}
	return true
}

func (h *RuntimeConfigHandler) requireFlow(w http.ResponseWriter, r *http.Request) bool {
	flowID := r.PathValue("flow_id")
	spec, err := h.flows.GetSpec(r.Context(), r.PathValue("namespace"), flowID)
	if err != nil || spec == nil {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (h *RuntimeConfigHandler) load(ctx context.Context, namespace, scopeType, scopeID string) (*entities.RuntimeConfiguration, error) {
	item, err := h.repo.Get(ctx, namespace, scopeType, scopeID)
	if errors.Is(err, runtimeconfig.ErrNotFound) {
		return emptyRuntimeConfiguration(namespace, scopeType, scopeID), nil
	}
	return item, err
}

func (h *RuntimeConfigHandler) resolveSecrets(ctx context.Context, item *entities.RuntimeConfiguration) (map[string]string, error) {
	if item == nil || item.SealedSecrets == nil {
		return map[string]string{}, nil
	}
	var secrets map[string]string
	if err := secretbox.OpenJSON(ctx, h.secretBox, item.SealedSecrets, runtimeConfigAAD(item), &secrets); err != nil {
		return nil, fmt.Errorf("resolve runtime secrets: %w", err)
	}
	if secrets == nil {
		secrets = map[string]string{}
	}
	return secrets, nil
}

func (h *RuntimeConfigHandler) writeRuntimeError(w http.ResponseWriter, err error) {
	if errors.Is(err, runtimeconfig.ErrNotFound) {
		http.Error(w, "runtime configuration not found", http.StatusNotFound)
		return
	}
	http.Error(w, "runtime configuration unavailable", http.StatusInternalServerError)
}

func emptyRuntimeConfiguration(namespace, scopeType, scopeID string) *entities.RuntimeConfiguration {
	return &entities.RuntimeConfiguration{
		BaseEntity: entities.BaseEntity{Namespace: namespace}, ScopeType: scopeType, ScopeID: scopeID,
		Environment: map[string]string{}, ConfiguredSecretKeys: []string{},
	}
}

func runtimeConfigAAD(item *entities.RuntimeConfiguration) []byte {
	return []byte(item.Namespace + "\x00" + item.ScopeType + "\x00" + item.ScopeID)
}

func runtimeConfigView(local, inherited *entities.RuntimeConfiguration, hasInheritance bool) entities.RuntimeConfigView {
	localLayer := runtimeConfigLayer(local)
	inheritedLayer := entities.RuntimeConfigLayer{Environment: map[string]string{}, SecretKeys: []string{}}
	if hasInheritance {
		inheritedLayer = runtimeConfigLayer(inherited)
	}
	effectiveEnvironment, effectiveSecrets := mergeRedacted(
		inheritedLayer.Environment, inheritedLayer.SecretKeys, localLayer.Environment, localLayer.SecretKeys,
	)
	return entities.RuntimeConfigView{
		Local: localLayer, Inherited: inheritedLayer,
		Effective: entities.RuntimeConfigLayer{Environment: effectiveEnvironment, SecretKeys: effectiveSecrets},
	}
}

func runtimeConfigLayer(item *entities.RuntimeConfiguration) entities.RuntimeConfigLayer {
	return entities.RuntimeConfigLayer{
		Environment: cloneStrings(item.Environment),
		SecretKeys:  append(make([]string, 0, len(item.ConfiguredSecretKeys)), item.ConfiguredSecretKeys...),
	}
}

func mergeRedacted(inheritedEnvironment map[string]string, inheritedSecrets []string, localEnvironment map[string]string, localSecrets []string) (map[string]string, []string) {
	environment := cloneStrings(inheritedEnvironment)
	secrets := make(map[string]struct{}, len(inheritedSecrets)+len(localSecrets))
	for _, key := range inheritedSecrets {
		secrets[key] = struct{}{}
		delete(environment, key)
	}
	for key, value := range localEnvironment {
		environment[key] = value
		delete(secrets, key)
	}
	for _, key := range localSecrets {
		secrets[key] = struct{}{}
		delete(environment, key)
	}
	keys := make([]string, 0, len(secrets))
	for key := range secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return environment, keys
}

func mergeResolved(inheritedEnvironment, inheritedSecrets, localEnvironment, localSecrets map[string]string) (map[string]string, map[string]string) {
	environment := cloneStrings(inheritedEnvironment)
	secrets := cloneStrings(inheritedSecrets)
	for key := range secrets {
		delete(environment, key)
	}
	for key, value := range localEnvironment {
		environment[key] = value
		delete(secrets, key)
	}
	for key, value := range localSecrets {
		secrets[key] = value
		delete(environment, key)
	}
	return environment, secrets
}

func validateRuntimeConfigUpdate(update *entities.RuntimeConfigUpdate) error {
	if update.Environment == nil {
		update.Environment = map[string]string{}
	}
	if len(update.Environment)+len(update.Secrets) > maxRuntimeConfigEntries {
		return fmt.Errorf("runtime configuration is limited to %d entries", maxRuntimeConfigEntries)
	}
	clear := make(map[string]struct{}, len(update.ClearSecretKeys))
	for _, key := range update.ClearSecretKeys {
		if err := validateRuntimeEnvironmentKey(key); err != nil {
			return err
		}
		if _, duplicate := clear[key]; duplicate {
			return fmt.Errorf("duplicate clear_secret_keys entry %q", key)
		}
		clear[key] = struct{}{}
	}
	for key, value := range update.Environment {
		if err := validateRuntimeEnvironmentKey(key); err != nil {
			return err
		}
		if len(value) > maxRuntimeConfigValueSize {
			return fmt.Errorf("value for %q exceeds %d bytes", key, maxRuntimeConfigValueSize)
		}
		if _, overlaps := update.Secrets[key]; overlaps {
			return fmt.Errorf("%q cannot be both an environment variable and a secret", key)
		}
	}
	for key, value := range update.Secrets {
		if err := validateRuntimeEnvironmentKey(key); err != nil {
			return err
		}
		if value == "" {
			return fmt.Errorf("secret %q must not be empty; use clear_secret_keys to remove it", key)
		}
		if len(value) > maxRuntimeConfigValueSize {
			return fmt.Errorf("secret %q exceeds %d bytes", key, maxRuntimeConfigValueSize)
		}
		if _, clearing := clear[key]; clearing {
			return fmt.Errorf("secret %q cannot be updated and cleared together", key)
		}
	}
	return nil
}

func validateRuntimeEnvironmentKey(key string) error {
	if !runtimeEnvironmentKey.MatchString(key) || len(key) > 128 {
		return fmt.Errorf("invalid environment key %q", key)
	}
	return nil
}

func cloneStrings(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeRuntimeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
