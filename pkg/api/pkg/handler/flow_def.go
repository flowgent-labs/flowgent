package handler

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
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
	store        store.IStore
	afStore      agentflow.IAgentFlowStore
	frStore      flowrun.IFlowRunStore
	logger       *utils.Logger
	agentFlows   map[string]*model.AgentFlowSpec
	mu           sync.RWMutex
	watchVersion int64
	watchChs     []chan struct{}
}

func NewFlowDefHandler(s store.IStore, logger *utils.Logger, agentFlows []model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) *FlowDefHandler {
	afMap := make(map[string]*model.AgentFlowSpec)
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
	return &FlowDefHandler{store: s, afStore: afStore, frStore: frStore, logger: logger, agentFlows: afMap, watchVersion: 1}
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
		json.NewEncoder(w).Encode(map[string]interface{}{"flows": []model.AgentFlowSpec{}, "version": cur})
	case <-r.Context().Done():
	}
}

func (h *FlowDefHandler) AgentFlows() map[string]*model.AgentFlowSpec {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := make(map[string]*model.AgentFlowSpec, len(h.agentFlows))
	for k, v := range h.agentFlows {
		c[k] = v
	}
	return c
}

func (h *FlowDefHandler) Reload(flows []model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) {
	h.mu.Lock()
	h.agentFlows = make(map[string]*model.AgentFlowSpec)
	for i := range flows {
		h.agentFlows[flows[i].ID] = &flows[i]
	}
	for k, v := range subFlows {
		h.agentFlows[k] = &v
	}
	h.mu.Unlock()
	h.notifyWatchers()
	log.Printf("[api] flow cache reloaded: %d flows", len(h.agentFlows))
}

func (h *FlowDefHandler) List(w http.ResponseWriter, r *http.Request) {
	defs, err := h.afStore.Select(r.Context(), model.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defs)
}

func (h *FlowDefHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	var spec model.AgentFlowSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if spec.ID == "" {
		http.Error(w, "id required", 400)
		return
	}
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
	var spec model.AgentFlowSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
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

func (h *FlowDefHandler) TriggerWithVars(w http.ResponseWriter, r *http.Request, agentFlowID string, vars map[string]any, trigger model.TriggerInfo) {
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
	run := &model.AgentFlowRun{AgentFlowID: agentFlowID, Version: 1, Status: model.RunPending, Vars: vars, Trigger: trigger}
	if err := h.frStore.Create(ctx, run); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"run_id": run.ID, "status": string(run.Status), "tenant": r.PathValue("tenant"), "agentflow_id": agentFlowID})
}

func (h *FlowDefHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentFlowID string            `json:"agentflow_id"`
		Vars        map[string]any    `json:"vars"`
		Trigger     model.TriggerInfo `json:"trigger"`
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
		Trigger model.TriggerInfo `json:"trigger"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	h.TriggerWithVars(w, r, r.PathValue("id"), req.Vars, req.Trigger)
}
