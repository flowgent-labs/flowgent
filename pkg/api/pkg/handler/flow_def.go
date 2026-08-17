package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/resourcepool"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var flowDefTracer = tracing.Tracer("flowgent/api/flowdef")

type ResourceAuthorizer interface {
	AuthorizeAdditional(*http.Request, string, string, string, string) (bool, error)
}

// FlowDefHandler manages flow definition CRUD, watch API, and in-memory cache.
type FlowDefHandler struct {
	store            store.IStore
	afStore          flow.IFlowInfoStore
	frStore          flowrun.IFlowRunStore
	poolStore        resourcepool.IRepository
	mqtt             MQTTPublisher
	logger           *utils.Logger
	agentFlows       map[string]*entities.FlowInfo
	namespacePrefix  string
	defaultNamespace string
	mu               sync.RWMutex
	watchVersion     int64
	watchChs         []chan struct{}
	poolAuthorizer   ResourceAuthorizer
}

// NewFlowDefHandler creates a FlowDefHandler. namespacePrefix is used to
// compute the default per-namespace K8s namespace for flows that don't set an
// explicit Namespace — it must match namespace.namespace_prefix so that Trigger
// (Path A) routes runs into the same namespace the Controller uses when
// creating the dedicated JM Deployment (see
// pkg/controller/pkg/controller.go runtimeNamespace). defaultNamespace is
// the fallback namespace ID (namespace.default_namespace) used when a flow spec
// doesn't carry its own Namespace.
func NewFlowDefHandler(s store.IStore, logger *utils.Logger, agentFlows []entities.FlowInfo, subFlows map[string]entities.FlowInfo, namespacePrefix string, defaultNamespace string, mqtt MQTTPublisher) *FlowDefHandler {
	afMap := make(map[string]*entities.FlowInfo)
	for i := range agentFlows {
		namespace := agentFlows[i].Namespace
		if namespace == "" {
			namespace = coalesceNamespace(defaultNamespace)
		}
		afMap[flowCacheKey(namespace, agentFlows[i].ID)] = &agentFlows[i]
	}
	for k, v := range subFlows {
		namespace := v.Namespace
		if namespace == "" {
			namespace = coalesceNamespace(defaultNamespace)
		}
		afMap[flowCacheKey(namespace, k)] = &v
	}

	var afStore flow.IFlowInfoStore
	var frStore flowrun.IFlowRunStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = flow.NewFlowPostgresStore(db)
		frStore = flowrun.NewFlowRunPostgresStore(db)
	case *sql.DB:
		afStore = flow.NewFlowSQLiteStore(db)
		frStore = flowrun.NewFlowRunSQLiteStore(db)
	}
	poolStore, _ := resourcepool.NewRepository(s)
	return &FlowDefHandler{store: s, afStore: afStore, frStore: frStore, poolStore: poolStore, logger: logger, agentFlows: afMap, namespacePrefix: defaultNamespacePrefix(namespacePrefix), defaultNamespace: coalesceNamespace(defaultNamespace), watchVersion: 1, mqtt: mqtt}
}

func flowCacheKey(namespace, id string) string { return namespace + "\x00" + id }

// SetPoolAuthorizer composes body-derived Resource Pool authorization without
// making Flow handlers depend on the IAM repository implementation.
func (h *FlowDefHandler) SetPoolAuthorizer(authorizer ResourceAuthorizer) {
	h.poolAuthorizer = authorizer
}

func (h *FlowDefHandler) authorizePoolUse(w http.ResponseWriter, r *http.Request, namespace, poolID string) bool {
	if h.poolAuthorizer == nil {
		return true
	}
	allowed, err := h.poolAuthorizer.AuthorizeAdditional(r, namespace, "resource_pool.use", "resource_pool", poolID)
	if err != nil {
		http.Error(w, "authorization service unavailable", http.StatusServiceUnavailable)
		return false
	}
	if !allowed {
		http.Error(w, "access denied for resource pool", http.StatusForbidden)
		return false
	}
	return true
}

// defaultNamespacePrefix falls back to "flowgent-" when unset, so
// Runtime routing never derives an unprefixed (and potentially colliding)
// namespace.
func defaultNamespacePrefix(prefix string) string {
	if prefix == "" {
		return "flowgent-"
	}
	return prefix
}

// coalesceNamespace falls back to "default" when unset, mirroring the
// namespace-fallback convention used by every cmd/ entrypoint (see e.g.
// pkg/cmd/pkg/controller/controller.go) — cfg.Runtime.Namespace.DefaultNamespace has no
// viper default of its own (pkg/config/pkg/config.go NamespaceConfig), so an
// omitted runtime.namespace block in the ConfigMap must still resolve consistently
// here and in the Controller (runtimeNamespace / c.namespace).
func coalesceNamespace(namespace string) string {
	if namespace == "" {
		return "default"
	}
	return namespace
}

func (h *FlowDefHandler) notifyWatchers() {
	h.mu.Lock()
	h.watchVersion++
	chs := h.watchChs
	h.watchChs = nil
	h.mu.Unlock()
	for _, ch := range chs {
		close(ch)
	}
}

func (h *FlowDefHandler) publishFlowEvent(ctx context.Context, eventType, flowID, namespaceID string) {
	if h.mqtt == nil {
		return
	}
	topic := fmt.Sprintf("flowgent/v1/%s/flows/%s/ctrl/flow/updated", namespaceID, flowID)
	if eventType == "DELETED" {
		topic = fmt.Sprintf("flowgent/v1/%s/flows/%s/ctrl/flow/deleted", namespaceID, flowID)
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"event_type":   eventType,
		"flow_id":      flowID,
		"namespace_id": namespaceID,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		slog.Warn("mqtt flow event publish failed", "topic", topic, "error", err)
	}
}

func (h *FlowDefHandler) Watch(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	h.mu.RLock()
	cur := h.watchVersion
	h.mu.RUnlock()
	if cur > since {
		h.List(w, r)
		return
	}
	ch := make(chan struct{})
	h.mu.Lock()
	h.watchChs = append(h.watchChs, ch)
	h.mu.Unlock()
	select {
	case <-ch:
		h.List(w, r)
	case <-time.After(30 * time.Second):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"flows": []entities.FlowInfo{}, "version": cur})
	case <-r.Context().Done():
	}
}

func (h *FlowDefHandler) AgentFlows() map[string]*entities.FlowInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := make(map[string]*entities.FlowInfo, len(h.agentFlows))
	for _, v := range h.agentFlows {
		if v != nil {
			c[v.ID] = v
		}
	}
	return c
}

// FlowStore exposes the definition repository to closely related API-domain
// handlers without duplicating backend selection logic.
func (h *FlowDefHandler) FlowStore() flow.IFlowInfoStore { return h.afStore }

// FlowRunStore exposes the run repository to API handlers that enforce
// scheduling referential integrity.
func (h *FlowDefHandler) FlowRunStore() flowrun.IFlowRunStore { return h.frStore }

func (h *FlowDefHandler) Reload(flows []entities.FlowInfo, subFlows map[string]entities.FlowInfo) {
	h.mu.Lock()
	h.agentFlows = make(map[string]*entities.FlowInfo)
	for i := range flows {
		h.agentFlows[flowCacheKey(h.logicalNamespace(&flows[i]), flows[i].ID)] = &flows[i]
	}
	for k, v := range subFlows {
		h.agentFlows[flowCacheKey(h.logicalNamespace(&v), k)] = &v
	}
	h.mu.Unlock()
	h.notifyWatchers()
	slog.Debug("api flow cache reloaded", "count", len(h.agentFlows))
}

// CacheDefinition publishes one already-durable definition to the runtime
// cache. Callers must persist (and commit) the definition first.
func (h *FlowDefHandler) CacheDefinition(spec *entities.FlowInfo) {
	if spec == nil {
		return
	}
	copy := *spec
	namespace := h.logicalNamespace(&copy)
	copy.Namespace = namespace
	h.mu.Lock()
	h.agentFlows[flowCacheKey(namespace, copy.ID)] = &copy
	h.mu.Unlock()
	h.notifyWatchers()
}

func (h *FlowDefHandler) List(w http.ResponseWriter, r *http.Request) {
	h.listDefinitions(w, r, false)
}

// ListSkills returns runtime kind=skill DAG definitions. Portable SKILL.md
// packages have a separate lifecycle and are intentionally not served here.
func (h *FlowDefHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	h.listDefinitions(w, r, true)
}

func (h *FlowDefHandler) listDefinitions(w http.ResponseWriter, r *http.Request, skills bool) {
	namespace := r.PathValue("namespace")
	defs, err := h.afStore.Select(r.Context(), namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := defs.Items
	if items == nil {
		items = []*entities.FlowVersionInfo{}
	}
	filtered := make([]*entities.FlowVersionInfo, 0, len(items))
	for _, item := range items {
		if item == nil || item.Namespace != namespace {
			continue
		}
		var spec entities.FlowInfo
		isSkill := len(item.Definition) > 0 && json.Unmarshal(item.Definition, &spec) == nil && strings.EqualFold(spec.Kind, "skill")
		if isSkill != skills {
			continue
		}
		filtered = append(filtered, item)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
}

func (h *FlowDefHandler) Create(w http.ResponseWriter, r *http.Request) {
	h.createDefinition(w, r, "")
}

func (h *FlowDefHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	h.createDefinition(w, r, "skill")
}

func (h *FlowDefHandler) createDefinition(w http.ResponseWriter, r *http.Request, forceKind string) {
	namespace := r.PathValue("namespace")
	var spec entities.FlowInfo
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if forceKind == "" {
		if err := resourceid.Validate(spec.ID); err != nil {
			http.Error(w, "invalid flow name: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else if spec.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if forceKind != "" {
		if spec.Kind != "" && !strings.EqualFold(spec.Kind, forceKind) {
			http.Error(w, "runtime skill kind must be skill", http.StatusBadRequest)
			return
		}
		spec.Kind = forceKind
	} else if spec.Kind == "" {
		spec.Kind = "flow"
	}
	spec.Namespace = namespace
	if forceKind == "" {
		if spec.ResourcePoolID == "" {
			spec.ResourcePoolID = "default"
		}
		if _, err := h.poolStore.Get(r.Context(), namespace, spec.ResourcePoolID); err != nil {
			http.Error(w, "resource_pool_id must reference an active pool in this namespace", http.StatusBadRequest)
			return
		}
		if !h.authorizePoolUse(w, r, namespace, spec.ResourcePoolID) {
			return
		}
	}
	createdBy := authenticatedUserID(r.Context())
	if err := h.afStore.CreateSpec(r.Context(), &spec, createdBy, "API create"); err != nil {
		if errors.Is(err, flow.ErrAlreadyExists) {
			http.Error(w, "flow name already exists in this namespace", http.StatusConflict)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	h.mu.Lock()
	h.agentFlows[flowCacheKey(namespace, spec.ID)] = &spec
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(spec)
	h.notifyWatchers()
	h.publishFlowEvent(r.Context(), "CREATED", spec.ID, namespace)
}

func (h *FlowDefHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	h.mu.RLock()
	namespace := r.PathValue("namespace")
	cached, ok := h.agentFlows[flowCacheKey(namespace, id)]
	h.mu.RUnlock()

	if ok && cached != nil && cached.Namespace == namespace {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cached)
		return
	}

	spec, err := h.afStore.GetSpec(r.Context(), namespace, id)
	if err != nil {
		slog.Error("FlowDefHandler.GetSpec failed", "id", id, "error", err)
		http.Error(w, "not found", 404)
		return
	}
	if spec == nil {
		slog.Warn("FlowDefHandler.GetSpec returned nil", "id", id)
		http.Error(w, "not found", 404)
		return
	}
	if spec.Namespace != namespace {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Populate in-memory cache for subsequent reads.
	h.mu.Lock()
	h.agentFlows[flowCacheKey(namespace, id)] = spec
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (h *FlowDefHandler) GetSkill(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	spec, err := h.afStore.GetSpec(r.Context(), namespace, r.PathValue("id"))
	if err != nil || spec == nil || spec.Namespace != r.PathValue("namespace") || !strings.EqualFold(spec.Kind, "skill") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (h *FlowDefHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.updateDefinition(w, r, "")
}

func (h *FlowDefHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	h.updateDefinition(w, r, "skill")
}

func (h *FlowDefHandler) updateDefinition(w http.ResponseWriter, r *http.Request, requiredKind string) {
	namespace, id := r.PathValue("namespace"), r.PathValue("id")

	existing, err := h.afStore.GetSpec(r.Context(), namespace, id)
	if err != nil || existing == nil || existing.Namespace != namespace ||
		(requiredKind != "" && !strings.EqualFold(existing.Kind, requiredKind)) {
		http.Error(w, "not found", 404)
		return
	}

	var updates entities.FlowInfo
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	if updates.Description != "" {
		existing.Description = updates.Description
	}
	if updates.Nodes != nil {
		existing.Nodes = updates.Nodes
	}
	if updates.Edges != nil {
		existing.Edges = updates.Edges
	}
	if updates.ResourcePoolID != "" && requiredKind == "" {
		if _, err := h.poolStore.Get(r.Context(), namespace, updates.ResourcePoolID); err != nil {
			http.Error(w, "resource_pool_id must reference an active pool in this namespace", http.StatusBadRequest)
			return
		}
		if !h.authorizePoolUse(w, r, namespace, updates.ResourcePoolID) {
			return
		}
		if updates.ResourcePoolID != existing.ResourcePoolID {
			active, err := h.frStore.HasActiveForFlow(r.Context(), namespace, id)
			if err != nil {
				http.Error(w, "unable to verify active runs", http.StatusInternalServerError)
				return
			}
			if active {
				http.Error(w, "resource pool cannot change while the flow has active runs", http.StatusConflict)
				return
			}
		}
		existing.ResourcePoolID = updates.ResourcePoolID
	}
	if updates.Vars != nil {
		existing.Vars = updates.Vars
	}
	if updates.Summary != "" {
		existing.Summary = updates.Summary
	}
	if updates.InputSchema != nil {
		existing.InputSchema = updates.InputSchema
	}
	if updates.OutputSchema != nil {
		existing.OutputSchema = updates.OutputSchema
	}
	if updates.Triggers != nil {
		existing.Triggers = updates.Triggers
	}
	if updates.SandboxPolicy != nil {
		existing.SandboxPolicy = updates.SandboxPolicy
	}
	if updates.Labels != nil {
		existing.Labels = updates.Labels
	}
	if updates.Credentials != nil {
		existing.Credentials = updates.Credentials
	}
	existing.UpdatedAt = time.Now()

	createdBy := authenticatedUserID(r.Context())
	if err := h.afStore.SaveSpec(r.Context(), existing, createdBy, "API update"); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.mu.Lock()
	h.agentFlows[flowCacheKey(namespace, id)] = existing
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
	h.notifyWatchers()
	h.publishFlowEvent(r.Context(), "UPDATED", id, existing.Namespace)
}

func (h *FlowDefHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.deleteDefinition(w, r, "")
}

func (h *FlowDefHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	h.deleteDefinition(w, r, "skill")
}

func (h *FlowDefHandler) deleteDefinition(w http.ResponseWriter, r *http.Request, requiredKind string) {
	id := r.PathValue("id")
	namespace := r.PathValue("namespace")
	existing, err := h.afStore.GetSpec(r.Context(), namespace, id)
	if err != nil || existing == nil || existing.Namespace != r.PathValue("namespace") ||
		(requiredKind != "" && !strings.EqualFold(existing.Kind, requiredKind)) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.afStore.Delete(r.Context(), namespace, id); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.mu.Lock()
	delete(h.agentFlows, flowCacheKey(namespace, id))
	h.mu.Unlock()
	w.WriteHeader(204)
	h.notifyWatchers()
	h.publishFlowEvent(r.Context(), "DELETED", id, namespace)
}

func (h *FlowDefHandler) TriggerWithVars(w http.ResponseWriter, r *http.Request, agentFlowID string, vars map[string]any, trigger entities.TriggerInfo) {
	namespace := r.PathValue("namespace")
	runID, err := h.CreateRunFromTrigger(r.Context(), agentFlowID, namespace, vars, trigger)
	if err != nil {
		if err == errFlowNotFound {
			http.Error(w, "agentflow not found", 404)
			return
		}
		http.Error(w, "internal", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": string(entities.RunPending), "namespace": namespace, "agentflow_id": agentFlowID})
}

// errFlowNotFound is returned by CreateRunFromTrigger when the referenced
// agentflow doesn't exist (in cache or DB), so callers can map it to a 404.
var errFlowNotFound = fmt.Errorf("agentflow not found")

// CreateRunFromTrigger is the shared, transport-agnostic run-creation path
// used by every trigger source (REST /flows/trigger, SCM webhook, A2A). It
// resolves the flow spec, creates a PENDING run in the flow's logical tenant
// namespace while recording its dedicated JobManager K8s namespace,
// persists it via the sole DB client, publishes a
// `ctrl/run/created` lifecycle event on MQTT, and returns the new run ID.
//
// Keeping this in one place guarantees every entry point routes runs to the
// same namespace the Controller's dedicated JM polls (see runtimeNamespace)
// and emits the same lifecycle event — the async half of Phase 1 (the JM
// run-poller → JobMaster DAG parse → topological TM dispatch) then proceeds
// identically regardless of how the run was triggered.
func (h *FlowDefHandler) CreateRunFromTrigger(ctx context.Context, agentFlowID, namespace string, vars map[string]any, trigger entities.TriggerInfo) (string, error) {
	ctx, span := flowDefTracer.Start(ctx, "FlowDefHandler.Trigger", trace.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
	defer span.End()

	h.mu.RLock()
	spec := h.agentFlows[flowCacheKey(namespace, agentFlowID)]
	h.mu.RUnlock()
	if spec == nil && h.afStore != nil {
		spec, _ = h.afStore.GetSpec(ctx, namespace, agentFlowID)
	}
	if spec == nil {
		return "", errFlowNotFound
	}

	namespaceID := h.logicalNamespace(spec)
	if namespace != namespaceID {
		// Treat cross-tenant trigger attempts as not found, consistent with the
		// rest of the namespaced resource API and without disclosing IDs.
		return "", errFlowNotFound
	}
	if spec.ResourcePoolID == "" {
		spec.ResourcePoolID = "default"
	}
	run := &entities.FlowRunInfo{AgentFlowID: agentFlowID, Version: 1, Status: entities.RunPending, Vars: vars, ResourcePoolID: spec.ResourcePoolID}
	// Every flow has a dedicated per-flow JM Deployment that only polls its
	// own namespace (see pkg/controller/pkg/controller.go
	// ensureFlowJobManager / runtimeNamespace).
	// BaseEntity.Namespace is the logical tenant boundary used by every
	// namespaced REST route. K8sNamespace is the physical dispatch target used
	// only by the dedicated JM poller. Conflating these fields makes a run
	// invisible to the tenant immediately after a successful trigger.
	run.Namespace = namespaceID
	run.K8sNamespace = h.runtimeNamespace(spec)
	run.SetTrigger(trigger)
	if err := h.frStore.Create(ctx, run); err != nil {
		return "", err
	}
	h.publishRunCreatedEvent(ctx, namespaceID, agentFlowID, run)
	return run.ID, nil
}

// publishRunCreatedEvent emits the `ctrl/run/created` lifecycle event on MQTT
// (docs §2.4 / VERIFICATION.md §4.2.4). Only the apiserver publishes ctrl/*
// events. Best-effort: a broker outage never fails run creation (the run is
// already persisted and the JM poller will still pick it up).
func (h *FlowDefHandler) publishRunCreatedEvent(ctx context.Context, namespaceID, flowID string, run *entities.FlowRunInfo) {
	if h.mqtt == nil {
		return
	}
	if namespaceID == "" {
		namespaceID = h.defaultNamespace
	}
	topic := fmt.Sprintf("flowgent/v1/%s/flows/%s/runs/%s/ctrl/run/created", namespaceID, flowID, run.ID)
	payload, _ := json.Marshal(map[string]any{
		"action":       "created",
		"run_id":       run.ID,
		"agentflow_id": flowID,
		"namespace_id": namespaceID,
		"namespace":    run.K8sNamespace,
		"trigger_type": run.TriggerType,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		slog.Warn("mqtt run created event publish failed", "topic", topic, "error", err)
	}
}

// runtimeNamespace mirrors pkg/controller/pkg/controller.go's runtimeNamespace
// so trigger creation and the Controller agree on which namespace a flow's
// dedicated JM lives in.
// Per §1.3/§4.3 of docs/01-L1-Engine-Architecture.md, namespace isolation is
// per-TENANT namespace (not per-flow) — every flow belonging to the same
// namespace shares one namespace, with each flow's dedicated JM Deployment
// disambiguated by name (flowgent-jobmanager-{namespaceId}-{flowId}).
func (h *FlowDefHandler) runtimeNamespace(spec *entities.FlowInfo) string {
	if spec.K8sNamespace != "" {
		return spec.K8sNamespace
	}
	return resourceid.KubernetesName(strings.TrimSuffix(h.namespacePrefix, "-"), h.logicalNamespace(spec))
}

// logicalNamespace resolves the stable tenant namespace stored in
// BaseEntity.Namespace and used by namespaced REST authorization/filtering.
// It must never return the physical Kubernetes namespace.
func (h *FlowDefHandler) logicalNamespace(spec *entities.FlowInfo) string {
	if spec.Namespace != "" {
		return spec.Namespace
	}
	return h.defaultNamespace
}

func (h *FlowDefHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentFlowID string               `json:"agentflow_id"`
		Vars        map[string]any       `json:"vars"`
		Trigger     entities.TriggerInfo `json:"trigger"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	h.TriggerWithVars(w, r, req.AgentFlowID, req.Vars, req.Trigger)
}

func (h *FlowDefHandler) TriggerByID(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vars    map[string]any       `json:"vars"`
		Trigger entities.TriggerInfo `json:"trigger"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	h.TriggerWithVars(w, r, r.PathValue("id"), req.Vars, req.Trigger)
}
