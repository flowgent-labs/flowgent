package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/approval"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/knowledge"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HumanHandler manages human approval endpoints.
type HumanHandler struct {
	store       approval.IApprovalStore
	runs        flowrun.IFlowRunStore
	publication knowledge.IKnowledgeStore
	mqtt        MQTTPublisher
	logger      *utils.Logger
}

// MQTTPublisher is the subset of messager needed to publish lifecycle events.
type MQTTPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// NewHumanHandler creates a human approval HTTP handler.
func NewHumanHandler(s storage.IStorage, mqtt MQTTPublisher, logger *utils.Logger) *HumanHandler {
	var apStore approval.IApprovalStore
	var runStore flowrun.IFlowRunStore
	var publicationStore knowledge.IKnowledgeStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		apStore = approval.NewApprovalPostgresStore(db)
		runStore = flowrun.NewFlowRunPostgresStore(db)
		publicationStore = knowledge.NewKnowledgePostgresStore(db)
	case *sql.DB:
		apStore = approval.NewApprovalSQLiteStore(db)
		runStore = flowrun.NewFlowRunSQLiteStore(db)
		publicationStore = knowledge.NewKnowledgeSQLiteStore(db)
	}
	return &HumanHandler{store: apStore, runs: runStore, publication: publicationStore, mqtt: mqtt, logger: logger}
}

// ListRunApprovals returns pending approvals only after the URL namespace and
// run identity have been verified.
func (h *HumanHandler) ListRunApprovals(w http.ResponseWriter, r *http.Request) {
	run, ok := h.authorizedRun(w, r)
	if !ok {
		return
	}
	items, err := h.store.ListPending(r.Context(), r.PathValue("namespace"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := make([]*entities.ApprovalInfo, 0)
	for _, item := range items {
		item.NormalizeAliases()
		if item.RunID == run.ID {
			filtered = append(filtered, item)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
}

// CreateRunApproval is the only approval creation endpoint. The URL owns both
// namespace and run identity; body values cannot redirect the record.
func (h *HumanHandler) CreateRunApproval(w http.ResponseWriter, r *http.Request) {
	run, ok := h.authorizedRun(w, r)
	if !ok {
		return
	}
	var approval entities.ApprovalInfo
	if err := decodeStrictJSON(r, &approval); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	approval.RunID = run.ID
	approval.Namespace = run.Namespace
	approval.Status = "PENDING"
	approval.NormalizeAliases()
	if err := h.store.CreateApproval(r.Context(), &approval); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(approval)
}

func (h *HumanHandler) ListNamespaceApprovals(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListPending(r.Context(), r.PathValue("namespace"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// ResolveRunApproval resolves an approval through a namespace/run-scoped URL.
func (h *HumanHandler) ResolveRunApproval(w http.ResponseWriter, r *http.Request) {
	decision := r.PathValue("decision")
	if decision != "approve" && decision != "reject" {
		http.Error(w, "decision must be approve or reject", http.StatusBadRequest)
		return
	}
	h.resolveRunApproval(w, r, decision == "approve")
}

func (h *HumanHandler) resolveRunApproval(w http.ResponseWriter, r *http.Request, approved bool) {
	run, ok := h.authorizedRun(w, r)
	if !ok {
		return
	}
	item, err := h.store.Get(r.Context(), r.PathValue("approval_id"))
	if item != nil {
		item.NormalizeAliases()
	}
	if err != nil || item == nil || item.RunID != run.ID {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	if item.Type == "knowledge_publish" || item.Type == "instruction_publish" {
		if h.publication == nil {
			http.Error(w, "publication store is not configured", http.StatusServiceUnavailable)
			return
		}
		candidate, resolveErr := h.publication.ResolveCandidateApproval(r.Context(), item.ID, approved,
			authenticatedUserID(r.Context()), map[string]any{"decision": r.PathValue("decision")})
		if resolveErr != nil {
			status := http.StatusConflict
			if errors.Is(resolveErr, knowledge.ErrApprovalExpired) {
				status = http.StatusGone
			}
			http.Error(w, resolveErr.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": candidate.Status, "candidate": candidate})
		return
	}
	terminal := strings.ToLower(item.Status)
	wanted := "rejected"
	if approved {
		wanted = "approved"
	}
	if terminal != "pending" {
		if terminal == wanted {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"status": terminal})
			return
		}
		http.Error(w, "approval is already "+terminal, http.StatusConflict)
		return
	}
	item.Approved = &approved
	if approved {
		item.Status = "APPROVED"
	} else {
		item.Status = "REJECTED"
	}
	item.DecidedBy = authenticatedUserID(r.Context())
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
	run.NormalizeAliases()
	if flowID := r.PathValue("flow_id"); flowID != "" && run.FlowName != flowID && run.AgentFlowID != flowID {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	return run, true
}
