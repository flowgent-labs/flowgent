package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/resourcepool"
)

// ResourcePoolHandler owns the namespace-scoped capacity API. Runtime
// reconciliation consumes exactly the same persisted model through the API.
type ResourcePoolHandler struct {
	repository resourcepool.IRepository
	flows      flow.IFlowInfoStore
	runs       flowrun.IFlowRunStore
}

func NewResourcePoolHandler(repository resourcepool.IRepository, flows flow.IFlowInfoStore, runs flowrun.IFlowRunStore) *ResourcePoolHandler {
	return &ResourcePoolHandler{repository: repository, flows: flows, runs: runs}
}

func (h *ResourcePoolHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.repository.List(r.Context(), r.PathValue("namespace"))
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeResourcePoolJSON(w, http.StatusOK, items)
}

func (h *ResourcePoolHandler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.repository.Get(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if errors.Is(err, resourcepool.ErrNotFound) {
		http.Error(w, "resource pool not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeResourcePoolJSON(w, http.StatusOK, item)
}

func (h *ResourcePoolHandler) Create(w http.ResponseWriter, r *http.Request) {
	var item entities.ResourcePoolInfo
	if json.NewDecoder(r.Body).Decode(&item) != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	item.Namespace = r.PathValue("namespace")
	item.Name = strings.TrimSpace(item.Name)
	if err := validateResourcePool(&item); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	item.CreatedBy = authenticatedUserID(r.Context())
	if err := h.repository.Create(r.Context(), &item); err != nil {
		if errors.Is(err, resourcepool.ErrAlreadyExists) {
			http.Error(w, "resource pool name already exists in this namespace", http.StatusConflict)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeResourcePoolJSON(w, http.StatusCreated, &item)
}

func (h *ResourcePoolHandler) Update(w http.ResponseWriter, r *http.Request) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	current, err := h.repository.Get(r.Context(), namespace, name)
	if errors.Is(err, resourcepool.ErrNotFound) {
		http.Error(w, "resource pool not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	var update entities.ResourcePoolInfo
	if json.NewDecoder(r.Body).Decode(&update) != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	current.Replicas = update.Replicas
	current.SlotsPerPod = update.SlotsPerPod
	current.Resources = update.Resources
	current.SandboxReplicas = update.SandboxReplicas
	current.SandboxSlotsPerPod = update.SandboxSlotsPerPod
	current.SandboxResources = update.SandboxResources
	current.PriorityClassName = update.PriorityClassName
	current.NodeSelector = update.NodeSelector
	current.Description = update.Description
	if err := validateResourcePool(current); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	current.UpdatedBy = authenticatedUserID(r.Context())
	if err := h.repository.Update(r.Context(), current); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeResourcePoolJSON(w, http.StatusOK, current)
}

func (h *ResourcePoolHandler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	active, err := h.runs.HasActiveForPool(r.Context(), namespace, name)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if active {
		http.Error(w, "resource pool still owns active runs", http.StatusConflict)
		return
	}
	definitions, err := h.flows.Select(r.Context(), namespace, entities.PageRequest{Page: 1, Size: 10000})
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	for _, version := range definitions.Items {
		var spec entities.FlowInfo
		if version != nil && json.Unmarshal(version.Definition, &spec) == nil &&
			!strings.EqualFold(spec.Kind, "skill") && strings.EqualFold(spec.ResourcePoolID, name) {
			http.Error(w, "resource pool is still bound to flow "+spec.ID, http.StatusConflict)
			return
		}
	}
	if err := h.repository.Delete(r.Context(), namespace, name, authenticatedUserID(r.Context())); err != nil {
		if errors.Is(err, resourcepool.ErrNotFound) {
			http.Error(w, "resource pool not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateResourcePool(item *entities.ResourcePoolInfo) error {
	if err := resourceid.Validate(item.Name); err != nil {
		return err
	}
	entities.NormalizeResourcePool(item)
	if item.Replicas > 100 {
		return errors.New("replicas must not exceed 100")
	}
	if item.SlotsPerPod > 256 || item.SandboxSlotsPerPod > 256 {
		return errors.New("slots per pod must not exceed 256")
	}
	if item.SandboxReplicas > 100 {
		return errors.New("sandbox replicas must not exceed 100")
	}
	for label, resources := range map[string]*model.SandboxResources{
		"resources": item.Resources, "sandbox_resources": item.SandboxResources,
	} {
		if resources != nil && (!validResourceQuantity(resources.CPU) || !validResourceQuantity(resources.Memory)) {
			return errors.New(label + " must use positive Kubernetes CPU and memory quantities")
		}
	}
	if len(item.PriorityClassName) > 253 {
		return errors.New("priority_class_name must not exceed 253 characters")
	}
	for key, value := range item.NodeSelector {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return errors.New("node_selector keys and values must not be empty")
		}
	}
	return nil
}

var resourceQuantityPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[EPTGMK]i?|[kmun]|[eE][+-]?[0-9]+)?$`)

func validResourceQuantity(value string) bool {
	if value == "" {
		return true
	}
	match := resourceQuantityPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	return err == nil && number > 0
}

func writeResourcePoolJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
