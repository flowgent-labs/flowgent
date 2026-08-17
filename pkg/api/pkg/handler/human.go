package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/approval"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HumanHandler manages human approval endpoints.
type HumanHandler struct {
	store  approval.IApprovalStore
	runs   flowrun.IFlowRunStore
	mqtt   MQTTPublisher
	logger *utils.Logger
}

// MQTTPublisher is the subset of messager needed to publish lifecycle events.
type MQTTPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// NewHumanHandler creates a human approval HTTP handler.
func NewHumanHandler(s store.IStore, mqtt MQTTPublisher, logger *utils.Logger) *HumanHandler {
	var apStore approval.IApprovalStore
	var runStore flowrun.IFlowRunStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		apStore = approval.NewApprovalPostgresStore(db)
		runStore = flowrun.NewFlowRunPostgresStore(db)
	case *sql.DB:
		apStore = approval.NewApprovalSQLiteStore(db)
		runStore = flowrun.NewFlowRunSQLiteStore(db)
	}
	return &HumanHandler{store: apStore, runs: runStore, mqtt: mqtt, logger: logger}
}

// ListRunApprovals returns pending approvals only after the URL namespace and
// run identity have been verified. The legacy global list remains available
// for runtime compatibility, but browser clients must use this scoped route.
func (h *HumanHandler) ListRunApprovals(w http.ResponseWriter, r *http.Request) {
	run, ok := h.authorizedRun(w, r)
	if !ok {
		return
	}
	items, err := h.store.ListPending(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := make([]*entities.ApprovalInfo, 0)
	for _, item := range items {
		if item.AgentFlowRunID == run.ID {
			filtered = append(filtered, item)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
}

// ApproveRun resolves an approval through a namespace/run-scoped URL.
func (h *HumanHandler) ApproveRun(w http.ResponseWriter, r *http.Request) {
	h.resolveRunApproval(w, r, true)
}

// RejectRun rejects an approval through a namespace/run-scoped URL.
func (h *HumanHandler) RejectRun(w http.ResponseWriter, r *http.Request) {
	h.resolveRunApproval(w, r, false)
}

func (h *HumanHandler) resolveRunApproval(w http.ResponseWriter, r *http.Request, approved bool) {
	run, ok := h.authorizedRun(w, r)
	if !ok {
		return
	}
	item, err := h.store.Get(r.Context(), r.PathValue("token"))
	if err != nil || item == nil || item.AgentFlowRunID != run.ID || item.Status != "PENDING" {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	item.Approved = &approved
	if approved {
		item.Status = "APPROVED"
	} else {
		item.Status = "REJECTED"
	}
	if err := h.store.UpdateApproval(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": strings.ToLower(item.Status)})
}

func (h *HumanHandler) authorizedRun(w http.ResponseWriter, r *http.Request) (*entities.FlowRunInfo, bool) {
	if h.runs == nil {
		http.Error(w, "run store is not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	run, err := h.runs.Get(r.Context(), requestRunID(r))
	if err != nil || run == nil || run.Namespace != r.PathValue("namespace") {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	if flowID := r.PathValue("flow_id"); flowID != "" && run.AgentFlowID != flowID {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	return run, true
}

// Approve approves a human task by token.
func (h *HumanHandler) Approve(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	approval, err := h.store.Get(r.Context(), token)
	if err != nil || approval == nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	approved := true
	approval.Approved = &approved
	approval.Status = "APPROVED"
	if err := h.store.UpdateApproval(r.Context(), approval); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "approved"})
}

// CreateApproval creates a new human approval record.
func (h *HumanHandler) CreateApproval(w http.ResponseWriter, r *http.Request) {
	var approval entities.ApprovalInfo
	if err := json.NewDecoder(r.Body).Decode(&approval); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	approval.Status = "PENDING"
	if err := h.store.CreateApproval(r.Context(), &approval); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(approval)
}

// ListPendingApprovals returns all pending human approvals.
func (h *HumanHandler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListPending(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// Reject rejects a human task by token.
func (h *HumanHandler) Reject(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	approval, err := h.store.Get(r.Context(), token)
	if err != nil || approval == nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	approved := false
	approval.Approved = &approved
	approval.Status = "REJECTED"
	if err := h.store.UpdateApproval(r.Context(), approval); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "rejected"})
}
