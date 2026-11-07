package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/authz"
	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	flowreleasestore "github.com/flowgent-labs/flowgent/storage/pkg/flowrelease"
	"github.com/google/uuid"
)

const maxFlowReleaseBodyBytes = 1 << 20

var (
	releaseVersionPattern  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$`)
	bindingIdentityPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,254}$`)
)

// FlowReleaseHandler owns the immutable publish/share/install use case. The
// repository performs the atomic local-flow installation; FlowDefHandler is
// updated only after commit so its runtime cache never leads durable state.
type FlowReleaseHandler struct {
	repo     flowreleasestore.IRepository
	flowDefs *FlowDefHandler
}

func NewFlowReleaseHandler(repo flowreleasestore.IRepository, flowDefs *FlowDefHandler) *FlowReleaseHandler {
	return &FlowReleaseHandler{repo: repo, flowDefs: flowDefs}
}

func (h *FlowReleaseHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListAccessibleReleases(r.Context(), r.PathValue("namespace"))
	writeFlowReleaseResult(w, items, err)
}

func (h *FlowReleaseHandler) Get(w http.ResponseWriter, r *http.Request) {
	namespace, releaseID := r.PathValue("namespace"), r.PathValue("id")
	allowed, err := h.repo.CanAccessRelease(r.Context(), releaseID, namespace)
	if err != nil {
		writeFlowReleaseResult(w, nil, err)
		return
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	item, err := h.repo.GetRelease(r.Context(), releaseID)
	writeFlowReleaseResult(w, item, err)
}

func (h *FlowReleaseHandler) Publish(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var input struct {
		FlowID         string `json:"flow_id"`
		ReleaseVersion string `json:"release_version"`
		Visibility     string `json:"visibility"`
		Description    string `json:"description"`
	}
	if !decodeFlowReleaseBody(w, r, &input) {
		return
	}
	if !resourceid.IsValid(input.FlowID) || !releaseVersionPattern.MatchString(input.ReleaseVersion) {
		http.Error(w, "valid flow_id and release_version are required", http.StatusBadRequest)
		return
	}
	visibility := strings.ToUpper(input.Visibility)
	if visibility == "" {
		visibility = "PRIVATE"
	}
	if visibility != "PRIVATE" && visibility != "SHARED" {
		http.Error(w, "visibility must be PRIVATE or SHARED", http.StatusBadRequest)
		return
	}
	version, err := h.flowDefs.afStore.Get(r.Context(), namespace, input.FlowID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	spec, err := h.flowDefs.afStore.GetSpec(r.Context(), namespace, input.FlowID)
	if err != nil || spec == nil || spec.Namespace != namespace {
		http.NotFound(w, r)
		return
	}
	snapshot, err := portableFlowSnapshot(spec)
	if err != nil {
		http.Error(w, "invalid flow definition", http.StatusUnprocessableEntity)
		return
	}
	checksum, err := flowDefinitionChecksum(snapshot)
	if err != nil {
		writeFlowReleaseResult(w, nil, err)
		return
	}
	now := time.Now().UTC()
	release := &entities.FlowRelease{
		BaseEntity:     entities.BaseEntity{ID: uuid.NewString(), Description: strings.TrimSpace(input.Description), Namespace: namespace},
		FlowID:         version.FlowID,
		FlowName:       input.FlowID,
		FlowVersion:    version.Version,
		ReleaseVersion: input.ReleaseVersion,
		Definition:     *snapshot,
		Checksum:       checksum,
		Visibility:     visibility,
		PublishedAt:    now,
	}
	release.MarkCreated(authz.PrincipalID(r))
	if err := h.repo.SaveRelease(r.Context(), release); err != nil {
		http.Error(w, "release version or snapshot already exists", http.StatusConflict)
		return
	}
	writeFlowReleaseCreated(w, release)
}

func (h *FlowReleaseHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	namespace, releaseID := r.PathValue("namespace"), r.PathValue("id")
	release, err := h.repo.GetRelease(r.Context(), releaseID)
	if err != nil || release.Namespace != namespace {
		http.NotFound(w, r)
		return
	}
	if err := h.repo.RevokeRelease(r.Context(), namespace, releaseID, authz.PrincipalID(r)); err != nil {
		http.Error(w, "release cannot be revoked", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FlowReleaseHandler) ListGrants(w http.ResponseWriter, r *http.Request) {
	namespace, releaseID := r.PathValue("namespace"), r.PathValue("id")
	if !h.ownsRelease(w, r, namespace, releaseID) {
		return
	}
	items, err := h.repo.ListGrants(r.Context(), namespace, releaseID)
	writeFlowReleaseResult(w, items, err)
}

func (h *FlowReleaseHandler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	namespace, releaseID := r.PathValue("namespace"), r.PathValue("id")
	if !h.ownsRelease(w, r, namespace, releaseID) {
		return
	}
	var input struct {
		ConsumerNamespace string     `json:"consumer_namespace"`
		ExpiresAt         *time.Time `json:"expires_at"`
		Description       string     `json:"description"`
	}
	if !decodeFlowReleaseBody(w, r, &input) {
		return
	}
	if input.ConsumerNamespace == namespace || !resourceid.IsValidNamespace(input.ConsumerNamespace) {
		http.Error(w, "a different valid consumer_namespace is required", http.StatusBadRequest)
		return
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		http.Error(w, "expires_at must be in the future", http.StatusBadRequest)
		return
	}
	grant := &entities.FlowReleaseGrant{
		BaseEntity:        entities.BaseEntity{ID: uuid.NewString(), Description: strings.TrimSpace(input.Description), Namespace: namespace},
		ReleaseID:         releaseID,
		ConsumerNamespace: input.ConsumerNamespace,
		ExpiresAt:         input.ExpiresAt,
	}
	grant.MarkCreated(authz.PrincipalID(r))
	if err := h.repo.SaveGrant(r.Context(), grant); err != nil {
		http.Error(w, "grant already exists", http.StatusConflict)
		return
	}
	writeFlowReleaseCreated(w, grant)
}

func (h *FlowReleaseHandler) DeleteGrant(w http.ResponseWriter, r *http.Request) {
	namespace, releaseID := r.PathValue("namespace"), r.PathValue("id")
	if !h.ownsRelease(w, r, namespace, releaseID) {
		return
	}
	if err := h.repo.DeleteGrant(r.Context(), namespace, releaseID, r.PathValue("grant_id"), authz.PrincipalID(r)); err != nil {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FlowReleaseHandler) ListInstallations(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListInstallations(r.Context(), r.PathValue("namespace"))
	writeFlowReleaseResult(w, items, err)
}

func (h *FlowReleaseHandler) Install(w http.ResponseWriter, r *http.Request) {
	namespace, releaseID := r.PathValue("namespace"), r.PathValue("id")
	allowed, err := h.repo.CanAccessRelease(r.Context(), releaseID, namespace)
	if err != nil {
		writeFlowReleaseResult(w, nil, err)
		return
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	release, err := h.repo.GetRelease(r.Context(), releaseID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var input struct {
		InstalledFlowID  string            `json:"installed_flow_id"`
		ResourceBindings map[string]string `json:"resource_bindings"`
		Description      string            `json:"description"`
	}
	if !decodeFlowReleaseBody(w, r, &input) {
		return
	}
	if input.InstalledFlowID == "" {
		input.InstalledFlowID = release.FlowName
	}
	if !resourceid.IsValid(input.InstalledFlowID) {
		http.Error(w, "invalid installed_flow_id", http.StatusBadRequest)
		return
	}
	definition := release.Definition
	definition.ID = ""
	definition.Name = input.InstalledFlowID
	definition.Namespace = namespace
	definition.K8sNamespace = ""
	if err := applyFlowResourceBindings(&definition, input.ResourceBindings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	appliedChecksum, err := flowDefinitionChecksum(&definition)
	if err != nil {
		writeFlowReleaseResult(w, nil, err)
		return
	}
	installation := &entities.FlowInstallation{
		BaseEntity:        entities.BaseEntity{ID: uuid.NewString(), Description: strings.TrimSpace(input.Description), Namespace: namespace},
		ReleaseID:         release.ID,
		ReleaseVersion:    release.ReleaseVersion,
		ProducerNamespace: release.Namespace,
		InstalledFlowName: input.InstalledFlowID,
		ReleaseChecksum:   release.Checksum,
		AppliedChecksum:   appliedChecksum,
		ResourceBindings:  input.ResourceBindings,
		InstalledAt:       time.Now().UTC(),
	}
	if installation.ResourceBindings == nil {
		installation.ResourceBindings = map[string]string{}
	}
	installation.MarkCreated(authz.PrincipalID(r))
	if err := h.repo.Install(r.Context(), &definition, installation, authz.PrincipalID(r)); err != nil {
		slog.Warn("flow release install failed", "namespace", namespace, "release_id", releaseID, "error", err)
		http.Error(w, "installed_flow_id already exists or installation failed", http.StatusConflict)
		return
	}
	h.flowDefs.CacheDefinition(&definition)
	h.flowDefs.publishFlowEvent(r.Context(), "CREATED", definition.ResourceName(), namespace)
	writeFlowReleaseCreated(w, installation)
}

func (h *FlowReleaseHandler) ownsRelease(w http.ResponseWriter, r *http.Request, namespace, releaseID string) bool {
	release, err := h.repo.GetRelease(r.Context(), releaseID)
	if err != nil || release.Namespace != namespace {
		http.NotFound(w, r)
		return false
	}
	return true
}

func portableFlowSnapshot(source *entities.FlowInfo) (*entities.FlowInfo, error) {
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var snapshot entities.FlowInfo
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, err
	}
	snapshot.BaseEntity = entities.BaseEntity{
		ID: source.ID, Description: source.Description, Status: "ACTIVE",
	}
	snapshot.K8sNamespace = ""
	return &snapshot, nil
}

func flowDefinitionChecksum(definition *entities.FlowInfo) (string, error) {
	payload, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func applyFlowResourceBindings(definition *entities.FlowInfo, bindings map[string]string) error {
	if len(bindings) == 0 {
		return nil
	}
	used := make(map[string]bool, len(bindings))
	var applyNode func(*entities.Node) error
	apply := func(kind string, value *string) error {
		if *value == "" {
			return nil
		}
		key := kind + ":" + *value
		if replacement, ok := bindings[key]; ok {
			if !bindingIdentityPattern.MatchString(replacement) {
				return fmt.Errorf("binding %s has an invalid target", key)
			}
			*value = replacement
			used[key] = true
		}
		return nil
	}
	applyNode = func(node *entities.Node) error {
		for _, ref := range []struct {
			kind  string
			value *string
		}{{"agent", &node.Agent}, {"skill", &node.Skill}, {"tool", &node.Tool}, {"flow", &node.AgentFlowID}} {
			if err := apply(ref.kind, ref.value); err != nil {
				return err
			}
		}
		if node.Node != nil {
			return applyNode(node.Node)
		}
		return nil
	}
	for i := range definition.Nodes {
		if err := applyNode(&definition.Nodes[i]); err != nil {
			return err
		}
	}
	for key := range bindings {
		if !used[key] {
			return fmt.Errorf("binding %s does not match a flow resource reference", key)
		}
	}
	return nil
}

func decodeFlowReleaseBody(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFlowReleaseBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "request body must contain exactly one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

func writeFlowReleaseResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		slog.Error("flow release request failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("encode flow release response", "error", err)
	}
}

func writeFlowReleaseCreated(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("encode flow release response", "error", err)
	}
}
