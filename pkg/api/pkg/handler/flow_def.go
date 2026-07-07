package handler

import (
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
	"github.com/flowgent-labs/flowgent/store/pkg/agentflow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var flowDefTracer = tracing.Tracer("flowgent/api/flowdef")

// FlowDefHandler manages flow definition CRUD, watch API, and in-memory cache.
type FlowDefHandler struct {
	store           store.IStore
	afStore         agentflow.IAgentFlowStore
	frStore         flowrun.IFlowRunStore
	logger          *utils.Logger
	agentFlows      map[string]*entities.AgentFlowInfo
	namespacePrefix string
	defaultTenant   string
	mu              sync.RWMutex
	watchVersion    int64
	watchChs        []chan struct{}
}

// NewFlowDefHandler creates a FlowDefHandler. namespacePrefix is used to
// compute the default per-tenant K8s namespace for flows that don't set an
// explicit Namespace — it must match tenant.namespace_prefix so that Trigger
// (Path A) routes runs into the same namespace the Controller uses when
// creating the dedicated JM Deployment (see
// pkg/controller/pkg/controller.go applicationNamespace). defaultTenant is
// the fallback tenant ID (tenant.default_tenant) used when a flow spec
// doesn't carry its own TenantID.
func NewFlowDefHandler(s store.IStore, logger *utils.Logger, agentFlows []entities.AgentFlowInfo, subFlows map[string]entities.AgentFlowInfo, namespacePrefix string, defaultTenant string) *FlowDefHandler {
	afMap := make(map[string]*entities.AgentFlowInfo)
	for i := range agentFlows {
		afMap[agentFlows[i].ID] = &agentFlows[i]
	}
	for k, v := range subFlows {
		afMap[k] = &v
	}

	var afStore agentflow.IAgentFlowStore
	var frStore flowrun.IFlowRunStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = agentflow.NewAgentFlowPostgresStore(db)
		frStore = flowrun.NewFlowRunPostgresStore(db)
	case *sql.DB:
		afStore = agentflow.NewAgentFlowSQLiteStore(db)
		frStore = flowrun.NewFlowRunSQLiteStore(db)
	}
	return &FlowDefHandler{store: s, afStore: afStore, frStore: frStore, logger: logger, agentFlows: afMap, namespacePrefix: defaultNamespacePrefix(namespacePrefix), defaultTenant: defaultTenantID(defaultTenant), watchVersion: 1}
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

// defaultTenantID falls back to "default" when unset, mirroring the
// tenant-fallback convention used by every cmd/ entrypoint (see e.g.
// pkg/cmd/pkg/controller/controller.go) — cfg.Tenant.DefaultTenant has no
// viper default of its own (pkg/config/pkg/config.go TenantConfig), so an
// omitted tenant: block in the ConfigMap must still resolve consistently
// here and in the Controller (applicationNamespace / c.tenant).
func defaultTenantID(tenant string) string {
	if tenant == "" {
		return "default"
	}
	return tenant
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
		json.NewEncoder(w).Encode(map[string]interface{}{"flows": []entities.AgentFlowInfo{}, "version": cur})
	case <-r.Context().Done():
	}
}

func (h *FlowDefHandler) AgentFlows() map[string]*entities.AgentFlowInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := make(map[string]*entities.AgentFlowInfo, len(h.agentFlows))
	for k, v := range h.agentFlows {
		c[k] = v
	}
	return c
}

func (h *FlowDefHandler) Reload(flows []entities.AgentFlowInfo, subFlows map[string]entities.AgentFlowInfo) {
	h.mu.Lock()
	h.agentFlows = make(map[string]*entities.AgentFlowInfo)
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
		items = []*entities.AgentFlowVersionInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *FlowDefHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	var spec entities.AgentFlowInfo
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
	spec.TenantID = tenant
	createdBy, _ := r.Context().Value(CtxUserID).(string)
	if err := h.afStore.SaveSpec(r.Context(), &spec, createdBy, "API create"); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.agentFlows[spec.ID] = &spec
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(spec)
	h.notifyWatchers()
}

func (h *FlowDefHandler) Get(w http.ResponseWriter, r *http.Request) {
	spec, err := h.afStore.GetSpec(r.Context(), r.PathValue("id"))
	if err != nil || spec == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (h *FlowDefHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenant, id := r.PathValue("tenant"), r.PathValue("id")
	var spec entities.AgentFlowInfo
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	priority, err := normalizePriority(spec.Priority)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	spec.Priority = priority
	spec.ID, spec.TenantID = id, tenant
	createdBy, _ := r.Context().Value(CtxUserID).(string)
	if err := h.afStore.SaveSpec(r.Context(), &spec, createdBy, "API update"); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	h.agentFlows[id] = &spec
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
	h.notifyWatchers()
}

func (h *FlowDefHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.afStore.Delete(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	delete(h.agentFlows, r.PathValue("id"))
	w.WriteHeader(204)
	h.notifyWatchers()
}

func (h *FlowDefHandler) TriggerWithVars(w http.ResponseWriter, r *http.Request, agentFlowID string, vars map[string]any, trigger entities.TriggerInfo) {
	ctx, span := flowDefTracer.Start(r.Context(), "FlowDefHandler.Trigger", trace.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
	defer span.End()
	spec := h.agentFlows[agentFlowID]
	if spec == nil && h.store != nil {
		spec, _ = h.afStore.GetSpec(ctx, agentFlowID)
	}
	if spec == nil {
		http.Error(w, "agentflow not found", 404)
		return
	}
	run := &entities.FlowRunInfo{AgentFlowID: agentFlowID, Version: 1, Status: entities.RunPending, Vars: vars, Priority: spec.Priority}
	// Every flow has a dedicated per-flow JM Deployment that only polls its
	// own namespace (see pkg/controller/pkg/controller.go
	// ensureApplicationInfra / applicationNamespace) — Session mode (a
	// shared JM pool picking up namespace="" runs) is currently disabled.
	// Route the run there instead of namespace="", which nothing would ever
	// pick up.
	run.Namespace = h.applicationNamespace(spec)
	run.SetTrigger(trigger)
	if err := h.frStore.Create(ctx, run); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"run_id": run.ID, "status": string(run.Status), "tenant": r.PathValue("tenant"), "agentflow_id": agentFlowID})
}

// applicationNamespace mirrors pkg/controller/pkg/controller.go's
// applicationNamespace so Trigger (Path A) and the Controller (Path B) agree
// on which namespace a given Application-mode flow's dedicated JM lives in.
// Per §1.3/§4.3 of docs/01-L1-Engine-Architecture.md, tenant isolation is
// per-TENANT namespace (not per-flow) — every flow belonging to the same
// tenant shares one namespace, with each flow's dedicated JM Deployment
// disambiguated by name (flowgent-jobmanager-{tenantId}-{flowId}).
func (h *FlowDefHandler) applicationNamespace(spec *entities.AgentFlowInfo) string {
	if spec.Namespace != "" {
		return spec.Namespace
	}
	tenantID := spec.TenantID
	if tenantID == "" {
		tenantID = h.defaultTenant
	}
	return h.namespacePrefix + tenantID
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
