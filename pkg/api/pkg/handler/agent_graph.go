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
	"github.com/flowgent-labs/flowgent/storage/pkg"
	graph "github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var graphDefTracer = tracing.Tracer("flowgent/api/flowdef")

// AgentGraphHandler manages graph definition CRUD, watch API, and in-memory cache.
type AgentGraphHandler struct {
	store            storage.IStorage
	afStore          graph.IAgentGraphStore
	frStore          graphrun.IGraphRunStore
	mqtt             MQTTPublisher
	logger           *utils.Logger
	agentGraphs      map[string]*entities.AgentGraph
	namespacePrefix  string
	defaultNamespace string
	sessionNamespace string
	mu               sync.RWMutex
	watchCursor      int64
	watchChs         []chan struct{}
}

// NewAgentGraphHandler creates a AgentGraphHandler. namespacePrefix is used to
// compute the default per-namespace K8s namespace for graphs that don't set an
// explicit Namespace — it must match namespace.namespace_prefix so that Trigger
// (Path A) routes runs into the same namespace the Controller uses when
// creating the application JM Deployment. defaultNamespace is the fallback
// namespace ID (namespace.default_namespace) used when a graph spec doesn't
// carry its own Namespace. sessionNamespace is the physical K8s namespace of
// the Helm-deployed session runtime cluster.
func NewAgentGraphHandler(s storage.IStorage, logger *utils.Logger, agentGraphs []entities.AgentGraph, subAgentGraphs map[string]entities.AgentGraph, namespacePrefix string, defaultNamespace string, sessionNamespace string, mqtt MQTTPublisher) *AgentGraphHandler {
	afMap := make(map[string]*entities.AgentGraph)
	for i := range agentGraphs {
		namespace := agentGraphs[i].Namespace
		if namespace == "" {
			namespace = coalesceNamespace(defaultNamespace)
		}
		putAgentGraphCacheAliases(afMap, namespace, &agentGraphs[i])
	}
	for k, v := range subAgentGraphs {
		namespace := v.Namespace
		if namespace == "" {
			namespace = coalesceNamespace(defaultNamespace)
		}
		putAgentGraphCacheAliases(afMap, namespace, &v)
		if k != "" {
			afMap[graphCacheKey(namespace, k)] = &v
		}
	}

	var afStore graph.IAgentGraphStore
	var frStore graphrun.IGraphRunStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = graph.NewAgentGraphPostgresStore(db)
		frStore = graphrun.NewGraphRunPostgresStore(db)
	case *sql.DB:
		afStore = graph.NewAgentGraphSQLiteStore(db)
		frStore = graphrun.NewGraphRunSQLiteStore(db)
	}
	return &AgentGraphHandler{
		store:            s,
		afStore:          afStore,
		frStore:          frStore,
		logger:           logger,
		agentGraphs:      afMap,
		namespacePrefix:  defaultNamespacePrefix(namespacePrefix),
		defaultNamespace: coalesceNamespace(defaultNamespace),
		sessionNamespace: coalesceNamespace(sessionNamespace),
		watchCursor:      1,
		mqtt:             mqtt,
	}
}

func graphCacheKey(namespace, id string) string { return namespace + "\x00" + id }

// putAgentGraphCacheAliases indexes a definition by both its namespace-local name
// and its stable database ID. The API accepts either identity on read/update
// routes, so the cache must mirror the store's lookup semantics.
func putAgentGraphCacheAliases(cache map[string]*entities.AgentGraph, namespace string, spec *entities.AgentGraph) {
	if spec == nil {
		return
	}
	if name := spec.ResourceName(); name != "" {
		cache[graphCacheKey(namespace, name)] = spec
	}
	if spec.ID != "" {
		cache[graphCacheKey(namespace, spec.ID)] = spec
	}
}

func deleteAgentGraphCacheAliases(cache map[string]*entities.AgentGraph, namespace string, spec *entities.AgentGraph, extraKeys ...string) {
	for _, key := range extraKeys {
		if key != "" {
			delete(cache, graphCacheKey(namespace, key))
		}
	}
	if spec == nil {
		return
	}
	if name := spec.ResourceName(); name != "" {
		delete(cache, graphCacheKey(namespace, name))
	}
	if spec.ID != "" {
		delete(cache, graphCacheKey(namespace, spec.ID))
	}
}

func (h *AgentGraphHandler) replaceCachedDefinition(namespace string, spec *entities.AgentGraph, staleKeys ...string) {
	if spec == nil {
		return
	}
	copy := *spec
	copy.Namespace = namespace
	h.mu.Lock()
	deleteAgentGraphCacheAliases(h.agentGraphs, namespace, nil, staleKeys...)
	putAgentGraphCacheAliases(h.agentGraphs, namespace, &copy)
	h.mu.Unlock()
}

func (h *AgentGraphHandler) removeCachedDefinition(namespace string, spec *entities.AgentGraph, routeKey string) {
	h.mu.Lock()
	deleteAgentGraphCacheAliases(h.agentGraphs, namespace, spec, routeKey)
	h.mu.Unlock()
}

// applyStableAgentGraphIdentity preserves the stable database identity while
// treating name as a separately mutable, namespace-local resource key.
func applyStableAgentGraphIdentity(existing, updates *entities.AgentGraph) {
	updates.ID = existing.ID
	if updates.Name == "" {
		updates.Name = existing.ResourceName()
	}
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

func (h *AgentGraphHandler) notifyWatchers() {
	h.mu.Lock()
	h.watchCursor++
	chs := h.watchChs
	h.watchChs = nil
	h.mu.Unlock()
	for _, ch := range chs {
		close(ch)
	}
}

func (h *AgentGraphHandler) publishAgentGraphEvent(ctx context.Context, eventType string, graph *entities.AgentGraph) {
	graphID, namespaceID := graph.ID, graph.Namespace
	if h.mqtt == nil {
		return
	}
	topic := fmt.Sprintf("flowgent/v1/%s/agent-graphs/%s/ctrl/graph/updated", namespaceID, graphID)
	if eventType == "DELETED" {
		topic = fmt.Sprintf("flowgent/v1/%s/agent-graphs/%s/ctrl/graph/deleted", namespaceID, graphID)
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"event_type":       eventType,
		"agent_graph_id":   graph.ID,
		"agent_graph_name": graph.Name,
		"namespace_id":     namespaceID,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		slog.Warn("mqtt graph event publish failed", "topic", topic, "error", err)
	}
}

func (h *AgentGraphHandler) Watch(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	h.mu.RLock()
	cur := h.watchCursor
	h.mu.RUnlock()
	if cur > since {
		h.writeWatchResponse(w, r, cur)
		return
	}
	ch := make(chan struct{})
	h.mu.Lock()
	h.watchChs = append(h.watchChs, ch)
	h.mu.Unlock()
	select {
	case <-ch:
		h.mu.RLock()
		version := h.watchCursor
		h.mu.RUnlock()
		h.writeWatchResponse(w, r, version)
	case <-time.After(30 * time.Second):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entities.AgentGraphWatchResponse{AgentGraphs: []entities.AgentGraph{}, Cursor: cur})
	case <-r.Context().Done():
	}
}

func (h *AgentGraphHandler) AgentGraphs() map[string]*entities.AgentGraph {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := make(map[string]*entities.AgentGraph, len(h.agentGraphs))
	for _, v := range h.agentGraphs {
		if v != nil {
			c[v.ResourceName()] = v
		}
	}
	return c
}

// AgentGraphStore exposes the definition repository to closely related API-domain
// handlers without duplicating backend selection logic.
func (h *AgentGraphHandler) AgentGraphStore() graph.IAgentGraphStore { return h.afStore }

// GraphRunStore exposes the run repository to API handlers that enforce
// scheduling referential integrity.
func (h *AgentGraphHandler) GraphRunStore() graphrun.IGraphRunStore { return h.frStore }

func (h *AgentGraphHandler) Reload(graphs []entities.AgentGraph, subAgentGraphs map[string]entities.AgentGraph) {
	h.mu.Lock()
	h.agentGraphs = make(map[string]*entities.AgentGraph)
	for i := range graphs {
		putAgentGraphCacheAliases(h.agentGraphs, h.logicalNamespace(&graphs[i]), &graphs[i])
	}
	for k, v := range subAgentGraphs {
		namespace := h.logicalNamespace(&v)
		putAgentGraphCacheAliases(h.agentGraphs, namespace, &v)
		if k != "" {
			h.agentGraphs[graphCacheKey(namespace, k)] = &v
		}
	}
	h.mu.Unlock()
	h.notifyWatchers()
	slog.Debug("api graph cache reloaded", "count", len(h.agentGraphs))
}

// CacheDefinition publishes one already-durable definition to the runtime
// cache. Callers must persist (and commit) the definition first.
func (h *AgentGraphHandler) CacheDefinition(spec *entities.AgentGraph) {
	if spec == nil {
		return
	}
	namespace := h.logicalNamespace(spec)
	h.replaceCachedDefinition(namespace, spec)
	h.notifyWatchers()
}

func (h *AgentGraphHandler) List(w http.ResponseWriter, r *http.Request) {
	h.listDefinitions(w, r, false)
}

// ListSkills returns runtime kind=skill DAG definitions. Portable SKILL.md
// packages have a separate lifecycle and are intentionally not served here.
func (h *AgentGraphHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	h.listDefinitions(w, r, true)
}

func (h *AgentGraphHandler) listDefinitions(w http.ResponseWriter, r *http.Request, skills bool) {
	items, err := h.listSpecs(r.Context(), r.PathValue("namespace"), skills)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *AgentGraphHandler) writeWatchResponse(w http.ResponseWriter, r *http.Request, version int64) {
	items, err := h.listSpecs(r.Context(), r.PathValue("namespace"), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entities.AgentGraphWatchResponse{AgentGraphs: items, Cursor: version})
}

func (h *AgentGraphHandler) listSpecs(ctx context.Context, namespace string, skills bool) ([]entities.AgentGraph, error) {
	defs, err := h.afStore.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	items := make([]entities.AgentGraph, 0, len(defs.Items))
	seen := make(map[string]struct{}, len(defs.Items))
	for _, definition := range defs.Items {
		if definition == nil || definition.Namespace != namespace {
			continue
		}
		if _, ok := seen[definition.AgentGraphID]; ok {
			continue
		}
		var spec entities.AgentGraph
		if err := json.Unmarshal(definition.Definition, &spec); err != nil {
			return nil, fmt.Errorf("decode graph %q: %w", definition.AgentGraphID, err)
		}
		spec.ID = definition.AgentGraphID
		spec.Name = definition.AgentGraphName
		if (spec.Kind == "skill") != skills {
			continue
		}
		spec.Namespace = namespace
		spec.Revision = definition.Revision
		spec.Status = definition.Status
		spec.CreatedAt = definition.CreatedAt
		spec.CreatedBy = definition.CreatedBy
		spec.UpdatedAt = definition.UpdatedAt
		spec.UpdatedBy = definition.UpdatedBy
		items = append(items, spec)
		seen[definition.AgentGraphID] = struct{}{}
	}
	return items, nil
}

func (h *AgentGraphHandler) Create(w http.ResponseWriter, r *http.Request) {
	h.createDefinition(w, r, "")
}

func (h *AgentGraphHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	h.createDefinition(w, r, "skill")
}

func (h *AgentGraphHandler) createDefinition(w http.ResponseWriter, r *http.Request, forceKind string) {
	namespace := r.PathValue("namespace")
	var spec entities.AgentGraph
	if err := decodeStrictJSON(r, &spec); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if spec.Namespace != "" && spec.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if forceKind == "" {
		if err := resourceid.Validate(spec.ResourceName()); err != nil {
			http.Error(w, "invalid graph name: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else if spec.ResourceName() == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if forceKind != "" {
		if spec.Kind != forceKind {
			http.Error(w, "runtime skill kind must be skill", http.StatusBadRequest)
			return
		}
	} else if spec.Kind != "graph" {
		http.Error(w, "graph kind must be graph", http.StatusBadRequest)
		return
	}
	spec.Namespace = namespace
	spec.Revision = 1
	if err := spec.ValidateDAG(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	if spec.Status == "" {
		spec.Status = "ACTIVE"
	}
	spec.CreatedAt = now
	spec.UpdatedAt = now
	if forceKind == "" {
		if err := entities.ValidateRuntimeMode(spec.RuntimeMode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	createdBy := authenticatedUserID(r.Context())
	if err := h.afStore.CreateSpec(r.Context(), &spec, createdBy, "API create"); err != nil {
		if errors.Is(err, graph.ErrAlreadyExists) {
			http.Error(w, "graph name already exists in this namespace", http.StatusConflict)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	h.replaceCachedDefinition(namespace, &spec)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(spec)
	h.notifyWatchers()
	h.publishAgentGraphEvent(r.Context(), "CREATED", &spec)
}

func (h *AgentGraphHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	h.mu.RLock()
	namespace := r.PathValue("namespace")
	cached, ok := h.agentGraphs[graphCacheKey(namespace, id)]
	h.mu.RUnlock()

	if ok && cached != nil && cached.Namespace == namespace {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cached)
		return
	}

	spec, err := h.afStore.GetSpec(r.Context(), namespace, id)
	if err != nil {
		slog.Error("AgentGraphHandler.GetSpec failed", "id", id, "error", err)
		http.Error(w, "not found", 404)
		return
	}
	if spec == nil {
		slog.Warn("AgentGraphHandler.GetSpec returned nil", "id", id)
		http.Error(w, "not found", 404)
		return
	}
	if spec.Namespace != namespace {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Populate in-memory cache for subsequent reads.
	h.replaceCachedDefinition(namespace, spec, id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (h *AgentGraphHandler) GetSkill(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	spec, err := h.afStore.GetSpec(r.Context(), namespace, r.PathValue("id"))
	if err != nil || spec == nil || spec.Namespace != r.PathValue("namespace") || spec.Kind != "skill" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (h *AgentGraphHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.updateDefinition(w, r, "")
}

func (h *AgentGraphHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	h.updateDefinition(w, r, "skill")
}

func (h *AgentGraphHandler) updateDefinition(w http.ResponseWriter, r *http.Request, requiredKind string) {
	namespace, id := r.PathValue("namespace"), r.PathValue("id")

	existing, err := h.afStore.GetSpec(r.Context(), namespace, id)
	if err != nil || existing == nil || existing.Namespace != namespace ||
		(requiredKind != "" && existing.Kind != requiredKind) {
		http.Error(w, "not found", 404)
		return
	}

	var updates entities.AgentGraph
	if err := decodeStrictJSON(r, &updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	if updates.ID != "" && updates.ID != id && updates.ID != existing.ID {
		http.Error(w, "id mismatch", http.StatusBadRequest)
		return
	}
	if updates.Namespace != "" && updates.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if requiredKind == "" {
		if updates.Kind != "graph" {
			http.Error(w, "graph kind must be graph", http.StatusBadRequest)
			return
		}
		updates.Kind = "graph"
		if err := entities.ValidateRuntimeMode(updates.RuntimeMode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if updates.RuntimeMode != existing.RuntimeMode {
			active, err := h.frStore.HasActiveForAgentGraph(r.Context(), namespace, id)
			if err != nil {
				http.Error(w, "unable to verify active runs", http.StatusInternalServerError)
				return
			}
			if active {
				http.Error(w, "runtime_mode cannot change while the graph has active runs", http.StatusConflict)
				return
			}
		}
	} else {
		if updates.Kind != requiredKind {
			http.Error(w, "runtime skill kind must be skill", http.StatusBadRequest)
			return
		}
	}
	applyStableAgentGraphIdentity(existing, &updates)
	updates.Namespace = namespace
	updates.Revision = existing.Revision
	updates.Status = existing.Status
	updates.CreatedAt = existing.CreatedAt
	updates.CreatedBy = existing.CreatedBy
	updates.UpdatedAt = time.Now()
	updates.UpdatedBy = authenticatedUserID(r.Context())
	if err := updates.ValidateDAG(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	createdBy := authenticatedUserID(r.Context())
	if err := h.afStore.SaveSpec(r.Context(), &updates, createdBy, "API update"); err != nil {
		slog.Error("AgentGraphHandler.SaveSpec failed", "id", id, "error", err)
		http.Error(w, "internal", 500)
		return
	}
	h.replaceCachedDefinition(namespace, &updates, id, existing.ResourceName(), existing.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updates)
	h.notifyWatchers()
	h.publishAgentGraphEvent(r.Context(), "UPDATED", &updates)
}

func (h *AgentGraphHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.deleteDefinition(w, r, "")
}

func (h *AgentGraphHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	h.deleteDefinition(w, r, "skill")
}

func (h *AgentGraphHandler) deleteDefinition(w http.ResponseWriter, r *http.Request, requiredKind string) {
	id := r.PathValue("id")
	namespace := r.PathValue("namespace")
	existing, err := h.afStore.GetSpec(r.Context(), namespace, id)
	if err != nil || existing == nil || existing.Namespace != r.PathValue("namespace") ||
		(requiredKind != "" && existing.Kind != requiredKind) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.afStore.Delete(r.Context(), namespace, id); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.removeCachedDefinition(namespace, existing, id)
	w.WriteHeader(204)
	h.notifyWatchers()
	h.publishAgentGraphEvent(r.Context(), "DELETED", existing)
}

func (h *AgentGraphHandler) TriggerWithInput(w http.ResponseWriter, r *http.Request, graphName string, input map[string]any, trigger entities.TriggerInfo) {
	namespace := r.PathValue("namespace")
	runID, err := h.CreateRunFromTrigger(r.Context(), graphName, namespace, input, trigger)
	if err != nil {
		if err == errAgentGraphNotFound {
			http.Error(w, "agentgraph not found", 404)
			return
		}
		http.Error(w, "internal", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	run, err := h.frStore.Get(r.Context(), runID)
	if err != nil {
		http.Error(w, "created graph run unavailable", 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"graph_run_id": runID, "status": string(entities.GraphRunPending), "namespace": namespace, "agent_graph_id": run.AgentGraphID, "agent_graph_name": run.AgentGraphName})
}

// errAgentGraphNotFound is returned by CreateRunFromTrigger when the referenced
// agentgraph doesn't exist (in cache or DB), so callers can map it to a 404.
var errAgentGraphNotFound = fmt.Errorf("agentgraph not found")

// CreateRunFromTrigger is the shared, transport-agnostic run-creation path
// used by every trigger source (REST /agent-graphs/trigger, SCM webhook, A2A). It
// resolves the graph spec, creates a PENDING run in the graph's logical tenant
// namespace while recording its dedicated JobManager K8s namespace,
// persists it via the sole DB client, publishes a
// `ctrl/run/created` lifecycle event on MQTT, and returns the new run ID.
//
// Keeping this in one place guarantees every entry point routes runs to the
// same namespace the Controller's dedicated JM polls (see runtimeNamespace)
// and emits the same lifecycle event — the async half of Phase 1 (the JM
// run-poller → JobMaster DAG parse → topological TM dispatch) then proceeds
// identically regardless of how the run was triggered.
func (h *AgentGraphHandler) CreateRunFromTrigger(ctx context.Context, agentGraphID, namespace string, vars map[string]any, trigger entities.TriggerInfo) (string, error) {
	ctx, span := graphDefTracer.Start(ctx, "AgentGraphHandler.Trigger", trace.WithAttributes(attribute.String("agent_graph_name", agentGraphID)))
	defer span.End()

	h.mu.RLock()
	spec := h.agentGraphs[graphCacheKey(namespace, agentGraphID)]
	h.mu.RUnlock()
	if spec == nil && h.afStore != nil {
		spec, _ = h.afStore.GetSpec(ctx, namespace, agentGraphID)
	}
	if spec == nil {
		return "", errAgentGraphNotFound
	}

	namespaceID := h.logicalNamespace(spec)
	if namespace != namespaceID {
		// Treat cross-tenant trigger attempts as not found, consistent with the
		// rest of the namespaced resource API and without disclosing IDs.
		return "", errAgentGraphNotFound
	}
	if err := entities.ValidateRuntimeMode(spec.RuntimeMode); err != nil {
		return "", fmt.Errorf("graph %q has invalid runtime_mode: %w", agentGraphID, err)
	}
	run := &entities.GraphRun{AgentGraphName: agentGraphID, AgentGraphRevision: 1, Status: entities.GraphRunPending, Input: vars, RuntimeMode: spec.RuntimeMode}
	// Application mode routes to the workload namespace where the Controller
	// creates the per-run JM. Session mode routes to the Helm-deployed session
	// runtime namespace.
	// BaseEntity.Namespace is the logical tenant boundary used by every
	// namespaced REST route. K8sNamespace is the physical dispatch target used
	// only by the JM poller. Conflating these fields makes a run
	// invisible to the tenant immediately after a successful trigger.
	run.Namespace = namespaceID
	run.K8sNamespace = h.runK8sNamespace(spec)
	run.SetTrigger(trigger)
	if err := h.frStore.Create(ctx, run); err != nil {
		return "", err
	}
	h.publishRunCreatedEvent(ctx, namespaceID, agentGraphID, run)
	return run.ID, nil
}

// publishRunCreatedEvent emits the `ctrl/run/created` lifecycle event on MQTT
// (docs §2.4 / VERIFICATION.md §4.2.4). Only the apiserver publishes ctrl/*
// events. Best-effort: a broker outage never fails run creation (the run is
// already persisted and the JM poller will still pick it up).
func (h *AgentGraphHandler) publishRunCreatedEvent(ctx context.Context, namespaceID, graphID string, run *entities.GraphRun) {
	if h.mqtt == nil {
		return
	}
	if namespaceID == "" {
		namespaceID = h.defaultNamespace
	}
	topic := fmt.Sprintf("flowgent/v1/%s/agent-graphs/%s/graph-runs/%s/ctrl/graph-run/created", namespaceID, run.AgentGraphID, run.ID)
	payload, _ := json.Marshal(map[string]any{
		"event_type":       "CREATED",
		"graph_run_id":     run.ID,
		"agent_graph_id":   run.AgentGraphID,
		"agent_graph_name": run.AgentGraphName,
		"namespace_id":     namespaceID,
		"namespace":        run.K8sNamespace,
		"status":           run.Status,
		"trigger_type":     run.TriggerType,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		slog.Warn("mqtt run created event publish failed", "topic", topic, "error", err)
	}
}

// runtimeNamespace mirrors pkg/controller/pkg/controller.go's runtimeNamespace
// so trigger creation and the Controller agree on which namespace a graph's
// dedicated JM lives in.
// Per §1.3/§4.3 of docs/01-L1-Engine-Architecture.md, namespace isolation is
// per-TENANT namespace (not per-graph) — every graph belonging to the same
// namespace shares one namespace, with each graph's dedicated JM Deployment
// disambiguated by name (flowgent-jobmanager-{namespaceId}-{graphId}).
func (h *AgentGraphHandler) runtimeNamespace(spec *entities.AgentGraph) string {
	if spec.K8sNamespace != "" {
		return spec.K8sNamespace
	}
	return resourceid.KubernetesName(strings.TrimSuffix(h.namespacePrefix, "-"), h.logicalNamespace(spec))
}

func (h *AgentGraphHandler) runK8sNamespace(spec *entities.AgentGraph) string {
	if spec != nil && spec.RuntimeMode == entities.RuntimeModeSession {
		return h.sessionNamespace
	}
	return h.runtimeNamespace(spec)
}

// logicalNamespace resolves the stable tenant namespace stored in
// BaseEntity.Namespace and used by namespaced REST authorization/filtering.
// It must never return the physical Kubernetes namespace.
func (h *AgentGraphHandler) logicalNamespace(spec *entities.AgentGraph) string {
	if spec.Namespace != "" {
		return spec.Namespace
	}
	return h.defaultNamespace
}

func (h *AgentGraphHandler) TriggerByID(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input   map[string]any       `json:"input"`
		Trigger entities.TriggerInfo `json:"trigger"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	h.TriggerWithInput(w, r, r.PathValue("id"), req.Input, req.Trigger)
}
