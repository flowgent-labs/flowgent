package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/approval"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HumanHandler manages human approval endpoints.
type HumanHandler struct {
	store  approval.IApprovalStore
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
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		apStore = approval.NewApprovalPostgresStore(db)
	case *sql.DB:
		apStore = approval.NewApprovalSQLiteStore(db)
	}
	return &HumanHandler{store: apStore, mqtt: mqtt, logger: logger}
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
	var approval model.HumanApproval
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
