package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var flowDefTracer = tracing.Tracer("flowgent/api/flowdef")

// FlowDefHandler manages flow definition CRUD, watch API, and in-memory cache.
type FlowDefHandler struct {
	store           store.IStore
	afStore         flow.IFlowInfoStore
	frStore         flowrun.IFlowRunStore
	mqtt            MQTTPublisher
	logger          *utils.Logger
	agentFlows      map[string]*entities.FlowInfo
	namespacePrefix string
	defaultNamespace   string
	mu              sync.RWMutex
	watchVersion    int64
	watchChs        []chan struct{}
}

// NewFlowDefHandler creates a FlowDefHandler. namespacePrefix is used to
// compute the default per-namespace K8s namespace for flows that don't set an
// explicit Namespace — it must match namespace.namespace_prefix so that Trigger
// (Path A) routes runs into the same namespace the Controller uses when
// creating the dedicated JM Deployment (see
// pkg/controller/pkg/controller.go applicationNamespace). defaultNamespace is
// the fallback namespace ID (namespace.default_namespace) used when a flow spec
// doesn't carry its own Namespace.
func NewFlowDefHandler(s store.IStore, logger *utils.Logger, agentFlows []entities.FlowInfo, subFlows map[string]entities.FlowInfo, namespacePrefix string, defaultNamespace string, mqtt MQTTPublisher) *FlowDefHandler {
	afMap := make(map[string]*entities.FlowInfo)
	for i := range agentFlows {
		afMap[agentFlows[i].ID] = &agentFlows[i]
	}
	for k, v := range subFlows {
		afMap[k] = &v
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
	return &FlowDefHandler{store: s, afStore: afStore, frStore: frStore, logger: logger, agentFlows: afMap, namespacePrefix: defaultNamespacePrefix(namespacePrefix), defaultNamespace: coalesceNamespace(defaultNamespace), watchVersion: 1, mqtt: mqtt}
}

// defaultNamespacePrefix falls back to "flowgent-" when unset, so
// Application-mode routing never derives an unprefixed (and potentially
// colliding) namespace.
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
// here and in the Controller (applicationNamespace / c.namespace).
func coalesceNamespace(namespace string) string {
	if namespace == "" {
		return "default"
	}
	return namespace
}

// normalizePriority defaults an unset Priority to PriorityHigh and rejects
// any other value. Session mode (low/medium priorities) is currently
// disabled — see entities.Priority doc comment — so PriorityHigh is the only
// value the API accepts; this keeps the field reserved on the wire/schema
// for a future Session-mode reintroduction without silently accepting values
// that nothing in the Controller/JM would honor.
func normalizePriority(p entities.Priority) (entities.Priority, error) {
	if p == "" {
		return entities.PriorityHigh, nil
	}
	if p != entities.PriorityHigh {
		return "", fmt.Errorf("priority %q is not supported (Session mode is disabled; only %q is currently accepted)", p, entities.PriorityHigh)
	}
	return p, nil
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
		"event_type": eventType,
		"flow_id":    flowID,
		"namespace_id":  namespaceID,
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
	for k, v := range h.agentFlows {
		c[k] = v
	}
	return c
}

func (h *FlowDefHandler) Reload(flows []entities.FlowInfo, subFlows map[string]entities.FlowInfo) {
	h.mu.Lock()
	h.agentFlows = make(map[string]*entities.FlowInfo)
	for i := range flows {
		h.agentFlows[flows[i].ID] = &flows[i]
	}
	for k, v := range subFlows {
		h.agentFlows[k] = &v
	}
	h.mu.Unlock()
	h.notifyWatchers()
	slog.Debug("api flow cache reloaded", "count", len(h.agentFlows))
}

func (h *FlowDefHandler) List(w http.ResponseWriter, r *http.Request) {
	defs, err := h.afStore.Select(r.Context(), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := defs.Items
	if items == nil {
		items = []*entities.FlowVersionInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *FlowDefHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var spec entities.FlowInfo
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if spec.ID == "" {
		http.Error(w, "id required", 400)
		return
	}
	priority, err := normalizePriority(spec.Priority)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	spec.Priority = priority
	spec.Namespace = namespace
	createdBy, _ := r.Context().Value(CtxUserID).(string)
	if err := h.afStore.SaveSpec(r.Context(), &spec, createdBy, "API create"); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.mu.Lock()
	h.agentFlows[spec.ID] = &spec
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
	cached, ok := h.agentFlows[id]
	h.mu.RUnlock()

	if ok && cached != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cached)
		return
	}

	spec, err := h.afStore.GetSpec(r.Context(), id)
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
	// Populate in-memory cache for subsequent reads.
	h.mu.Lock()
	h.agentFlows[id] = spec
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (h *FlowDefHandler) Update(w http.ResponseWriter, r *http.Request) {
	_, id := r.PathValue("namespace"), r.PathValue("id")

	existing, err := h.afStore.GetSpec(r.Context(), id)
	if err != nil || existing == nil {
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
	if updates.Priority != "" {
		priority, err := normalizePriority(updates.Priority)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		existing.Priority = priority
	}
	if updates.Vars != nil {
		existing.Vars = updates.Vars
	}
	existing.UpdatedAt = time.Now()

	createdBy, _ := r.Context().Value(CtxUserID).(string)
	if err := h.afStore.SaveSpec(r.Context(), existing, createdBy, "API update"); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.agentFlows[id] = existing
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
	h.notifyWatchers()
	h.publishFlowEvent(r.Context(), "UPDATED", id, existing.Namespace)
}

func (h *FlowDefHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.afStore.Delete(r.Context(), id); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	namespace := r.PathValue("namespace")
	delete(h.agentFlows, id)
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
// resolves the flow spec, creates a PENDING run in the flow's dedicated
// Application-mode namespace, persists it via the sole DB client, publishes a
// `ctrl/run/created` lifecycle event on MQTT, and returns the new run ID.
//
// Keeping this in one place guarantees every entry point routes runs to the
// same namespace the Controller's dedicated JM polls (see applicationNamespace)
// and emits the same lifecycle event — the async half of Phase 1 (the JM
// run-poller → JobMaster DAG parse → topological TM dispatch) then proceeds
// identically regardless of how the run was triggered.
func (h *FlowDefHandler) CreateRunFromTrigger(ctx context.Context, agentFlowID, namespace string, vars map[string]any, trigger entities.TriggerInfo) (string, error) {
	ctx, span := flowDefTracer.Start(ctx, "FlowDefHandler.Trigger", trace.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
	defer span.End()

	h.mu.RLock()
	spec := h.agentFlows[agentFlowID]
	h.mu.RUnlock()
	if spec == nil && h.afStore != nil {
		spec, _ = h.afStore.GetSpec(ctx, agentFlowID)
	}
	if spec == nil {
		return "", errFlowNotFound
	}

	run := &entities.FlowRunInfo{AgentFlowID: agentFlowID, Version: 1, Status: entities.RunPending, Vars: vars, Priority: spec.Priority}
	// Every flow has a dedicated per-flow JM Deployment that only polls its
	// own namespace (see pkg/controller/pkg/controller.go
	// ensureApplicationInfra / applicationNamespace) — Session mode (a
	// shared JM pool picking up namespace="" runs) is currently disabled.
	// Route the run there instead of namespace="", which nothing would ever
	// pick up.
	run.Namespace = h.applicationNamespace(spec)
	if run.Namespace == "" {
		run.Namespace = spec.Namespace
	}
	// Align K8sNamespace with Namespace so JM poller's k8s_namespace filter matches.
	// The poller filters runs by k8s_namespace (jobmanager.go:89) and without this,
	// runs with empty K8sNamespace get silently skipped.
	run.K8sNamespace = run.Namespace
	run.SetTrigger(trigger)
	if err := h.frStore.Create(ctx, run); err != nil {
		return "", err
	}
	h.publishRunCreatedEvent(ctx, agentFlowID, run)
	return run.ID, nil
}

// publishRunCreatedEvent emits the `ctrl/run/created` lifecycle event on MQTT
// (docs §2.4 / VERIFICATION.md §4.2.4). Only the apiserver publishes ctrl/*
// events. Best-effort: a broker outage never fails run creation (the run is
// already persisted and the JM poller will still pick it up).
func (h *FlowDefHandler) publishRunCreatedEvent(ctx context.Context, flowID string, run *entities.FlowRunInfo) {
	if h.mqtt == nil {
		return
	}
	namespaceID := run.Namespace
	if namespaceID == "" {
		namespaceID = h.defaultNamespace
	}
	topic := fmt.Sprintf("flowgent/v1/%s/flows/%s/runs/%s/ctrl/run/created", namespaceID, flowID, run.ID)
	payload, _ := json.Marshal(map[string]any{
		"action":       "created",
		"run_id":       run.ID,
		"agentflow_id": flowID,
		"namespace_id":    namespaceID,
		"namespace": run.K8sNamespace,
		"trigger_type": run.TriggerType,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		slog.Warn("mqtt run created event publish failed", "topic", topic, "error", err)
	}
}

// applicationNamespace mirrors pkg/controller/pkg/controller.go's
// applicationNamespace so Trigger (Path A) and the Controller (Path B) agree
// on which namespace a given Application-mode flow's dedicated JM lives in.
// Per §1.3/§4.3 of docs/01-L1-Engine-Architecture.md, namespace isolation is
// per-TENANT namespace (not per-flow) — every flow belonging to the same
// namespace shares one namespace, with each flow's dedicated JM Deployment
// disambiguated by name (flowgent-jobmanager-{namespaceId}-{flowId}).
func (h *FlowDefHandler) applicationNamespace(spec *entities.FlowInfo) string {
	if spec.K8sNamespace != "" {
		return spec.K8sNamespace
	}
	namespaceID := spec.Namespace
	if namespaceID == "" {
		namespaceID = h.defaultNamespace
	}
	return h.namespacePrefix + namespaceID
}

func (h *FlowDefHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentFlowID string            `json:"agentflow_id"`
		Vars        map[string]any    `json:"vars"`
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
		Vars    map[string]any    `json:"vars"`
		Trigger entities.TriggerInfo `json:"trigger"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	h.TriggerWithVars(w, r, r.PathValue("id"), req.Vars, req.Trigger)
}
