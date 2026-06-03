package handler

import (
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
)

// HumanHandler manages human approval endpoints.
type HumanHandler struct {
	store  model.HumanApprovalStore
	logger *utils.Logger
}

// NewHumanHandler creates a human approval HTTP handler.
func NewHumanHandler(s model.HumanApprovalStore, logger *utils.Logger) *HumanHandler {
	return &HumanHandler{store: s, logger: logger}
}

// Approve approves a human task by token.
func (h *HumanHandler) Approve(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	approval, err := h.store.GetHumanApproval(r.Context(), token)
	if err != nil || approval == nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	approved := true
	approval.Approved = &approved
	approval.Status = "APPROVED"
	if err := h.store.UpdateHumanApproval(r.Context(), approval); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "approved"})
}

// Reject rejects a human task by token.
func (h *HumanHandler) Reject(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	approval, err := h.store.GetHumanApproval(r.Context(), token)
	if err != nil || approval == nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	approved := false
	approval.Approved = &approved
	approval.Status = "REJECTED"
	if err := h.store.UpdateHumanApproval(r.Context(), approval); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "rejected"})
}
