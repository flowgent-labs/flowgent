package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/src/tracing"
	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var apiTracer = tracing.Tracer("flowgent/api")

// AgentFlowHandler manages agentflow HTTP endpoints.
// agentFlows is an in-memory cache of flow definitions, invalidated on CRUD.
// watchVersion increments on every create/update/delete, used for long-poll watch.
type AgentFlowHandler struct {
	store        store.Store
	logger       *utils.Logger
	agentFlows   map[string]*model.AgentFlowSpec
	mu           sync.RWMutex
	watchVersion int64
	watchChs     []chan struct{}
}

// NewAgentFlowHandler creates an agentflow HTTP handler.
func NewAgentFlowHandler(s store.Store, logger *utils.Logger, agentFlows []model.AgentFlowSpec, subAgentFlows map[string]model.AgentFlowSpec) *AgentFlowHandler {
	afMap := make(map[string]*model.AgentFlowSpec)
	for i := range agentFlows {
		afMap[agentFlows[i].ID] = &agentFlows[i]
	}
	for k := range subAgentFlows {
		sw := subAgentFlows[k]
		afMap[k] = &sw
	}
	return &AgentFlowHandler{store: s, logger: logger, agentFlows: afMap, watchVersion: 1}
}

// notifyWatchers wakes up all long-poll watchers.
func (h *AgentFlowHandler) notifyWatchers() {
	h.mu.Lock()
	h.watchVersion++
	chs := h.watchChs
	h.watchChs = nil
	h.mu.Unlock()
	for _, ch := range chs {
		close(ch)
	}
}

// Watch handles GET /agentflows?watch=true&since=N — long-poll for flow changes.
// Blocks up to 30s, returns immediately if version > since.
func (h *AgentFlowHandler) Watch(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	timeout := 30 * time.Second

	h.mu.RLock()
	currentVer := h.watchVersion
	h.mu.RUnlock()

	if currentVer > since {
		h.ListDefinitions(w, r)
		return
	}

	ch := make(chan struct{})
	h.mu.Lock()
	h.watchChs = append(h.watchChs, ch)
	h.mu.Unlock()

	select {
	case <-ch:
		h.ListDefinitions(w, r)
	case <-time.After(timeout):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"flows": []model.AgentFlowSpec{}, "version": currentVer})
	case <-r.Context().Done():
	}
}

// AgentFlows returns a copy of the in-memory flow cache.
func (h *AgentFlowHandler) AgentFlows() map[string]*model.AgentFlowSpec {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := make(map[string]*model.AgentFlowSpec, len(h.agentFlows))
	for k, v := range h.agentFlows { c[k] = v }
	return c
}

// Reload replaces the in-memory flow cache (called on hot-reload).
func (h *AgentFlowHandler) Reload(flows []model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) {
	h.mu.Lock()
	h.agentFlows = make(map[string]*model.AgentFlowSpec)
	for i := range flows { h.agentFlows[flows[i].ID] = &flows[i] }
	for k, v := range subFlows { h.agentFlows[k] = &v }
	h.mu.Unlock()
	h.notifyWatchers()
	log.Printf("[api] flow cache reloaded: %d flows", len(h.agentFlows))
}

// ─── Definition CRUD ───────────────────────────────────────

// ListDefinitions returns all saved agentflow definitions for the tenant.
func (h *AgentFlowHandler) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := h.store.ListAgentFlowDefinitions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defs)
}

// CreateDefinition persists a new agentflow specification from the request body.
func (h *AgentFlowHandler) CreateDefinition(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	var spec model.AgentFlowSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if spec.ID == "" {
		http.Error(w, "agentflow id is required", http.StatusBadRequest)
		return
	}
	spec.TenantID = tenant

	// Extract user from context if available
	createdBy := ""
	if uid, ok := r.Context().Value(CtxUserID).(string); ok {
		createdBy = uid
	}

	if err := h.store.UpdateAgentFlowSpec(r.Context(), &spec, createdBy, "API create"); err != nil {
		h.logger.Error("create agentflow", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Also update the in-memory map
	h.agentFlows[spec.ID] = &spec

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(spec)
	h.notifyWatchers()
}

// GetDefinition returns a single agentflow spec by ID.
func (h *AgentFlowHandler) GetDefinition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	spec, err := h.store.GetAgentFlowSpec(r.Context(), id)
	if err != nil || spec == nil {
		http.Error(w, "agentflow not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

// UpdateDefinition modifies an existing agentflow specification.
func (h *AgentFlowHandler) UpdateDefinition(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	id := r.PathValue("id")
	var spec model.AgentFlowSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	spec.ID = id
	spec.TenantID = tenant

	createdBy := ""
	if uid, ok := r.Context().Value(CtxUserID).(string); ok {
		createdBy = uid
	}

	if err := h.store.UpdateAgentFlowSpec(r.Context(), &spec, createdBy, "API update"); err != nil {
		h.logger.Error("update agentflow", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.agentFlows[id] = &spec
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
	h.notifyWatchers()
}

// DeleteDefinition removes an agentflow specification.
func (h *AgentFlowHandler) DeleteDefinition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteAgentFlowDefinition(r.Context(), id); err != nil {
		h.logger.Error("delete agentflow", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	delete(h.agentFlows, id)
	w.WriteHeader(http.StatusNoContent)
	h.notifyWatchers()
}

// ─── Trigger ───────────────────────────────────────────────

// TriggerWithVars starts a new agentflow run for the given spec.
func (h *AgentFlowHandler) TriggerWithVars(w http.ResponseWriter, r *http.Request, agentFlowID string, vars map[string]any, triggerInfo model.TriggerInfo) {
	ctx := r.Context()
	ctx, span := apiTracer.Start(ctx, "TriggerWithVars",
		trace.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
	defer span.End()

	tenant := r.PathValue("tenant")
	spec := h.agentFlows[agentFlowID]
	if spec == nil {
		if h.store != nil {
			if dbSpec, err := h.store.GetAgentFlowSpec(ctx, agentFlowID); err == nil && dbSpec != nil {
				spec = dbSpec
			}
		}
	}
	if spec == nil {
		http.Error(w, "agentflow not found", http.StatusNotFound)
		return
	}
	run := &model.AgentFlowRun{
		AgentFlowID: agentFlowID,
		Version:     1,
		Status:      model.RunPending,
		Vars:        vars,
		Trigger:     triggerInfo,
		TenantID:    tenant,
		Priority:    spec.Priority,
		Namespace:   spec.Namespace,
	}
	if err := h.store.CreateAgentFlowRun(ctx, run); err != nil {
		span.RecordError(err)
		h.logger.Error("create agentflow run", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.logger.Info("agentflow triggered", "id", run.ID, "agentflow", agentFlowID, "tenant", tenant)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"run_id": run.ID, "agentflow_id": agentFlowID, "status": run.Status, "tenant": tenant})
}

// Trigger starts a new agentflow run. Supports both {id} in path and agentflow_id in body.
func (h *AgentFlowHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentFlowID string            `json:"agentflow_id"`
		Vars        map[string]any    `json:"vars,omitempty"`
		Trigger     model.TriggerInfo `json:"trigger"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.AgentFlowID == "" {
		http.Error(w, "agentflow_id is required", http.StatusBadRequest)
		return
	}
	if req.Trigger.Type == "" {
		req.Trigger = model.TriggerInfo{Type: "api", Source: "rest"}
	}
	h.TriggerWithVars(w, r, req.AgentFlowID, req.Vars, req.Trigger)
}

// TriggerByID triggers a run for the agentflow specified by path ID.
func (h *AgentFlowHandler) TriggerByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Vars    map[string]any    `json:"vars,omitempty"`
		Trigger model.TriggerInfo `json:"trigger"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Empty body is fine; use defaults
		req = struct {
			Vars    map[string]any    `json:"vars,omitempty"`
			Trigger model.TriggerInfo `json:"trigger"`
		}{}
	}
	if req.Trigger.Type == "" {
		req.Trigger = model.TriggerInfo{Type: "api", Source: "rest"}
	}
	h.TriggerWithVars(w, r, id, req.Vars, req.Trigger)
}

// ─── Deprecated: Run methods moved to RunHandler ───────────
// Kept for backward compatibility; new code should use RunHandler.

func (h *AgentFlowHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	agentFlowID := r.URL.Query().Get("agentflow_id")
	runs, err := h.store.ListAgentFlowRuns(r.Context(), agentFlowID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = time.Now() // ensure import used
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (h *AgentFlowHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.store.GetAgentFlowRun(r.Context(), id)
	if err != nil || run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (h *AgentFlowHandler) GetTaskRuns(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	tasks, err := h.store.GetTaskRunsByAgentFlowRun(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}
