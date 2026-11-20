package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/task"
)

func (h *FlowRunHandler) InitializeSubflow(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var body struct {
		InstanceID string `json:"instance_id"`
	}
	if err := decodeStrictJSON(r, &body); err != nil || body.InstanceID == "" {
		http.Error(w, "instance_id is required", 400)
		return
	}
	state, err := h.subflows.Transact(r.Context(), r.PathValue("namespace"), requestRunID(r), body.InstanceID, nil)
	h.subflowResponse(w, state, err)
}
func (h *FlowRunHandler) ApplyGraphPatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var patch entities.GraphPatch
	if err := decodeStrictJSON(r, &patch); err != nil || patch.FlowRunID != requestRunID(r) {
		http.Error(w, "invalid patch", 400)
		return
	}
	state, err := h.subflows.Transact(r.Context(), r.PathValue("namespace"), requestRunID(r), patch.SubflowInstanceID, &patch)
	h.subflowResponse(w, state, err)
}
func (h *FlowRunHandler) subflowResponse(w http.ResponseWriter, state *entities.SubflowState, err error) {
	if err != nil {
		code := http.StatusUnprocessableEntity
		if errors.Is(err, flowrun.ErrGraphConflict) || strings.Contains(err.Error(), "defer_patch") {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// ClaimTask consumes the durable dispatch identity before a TM invokes tools.
func (h *FlowRunHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var body struct {
		NodeInstanceID string `json:"node_instance_id"`
		DispatchEpoch  int64  `json:"dispatch_epoch"`
		Owner          string `json:"owner"`
	}
	if err := decodeStrictJSON(r, &body); err != nil || body.Owner == "" || body.NodeInstanceID == "" || body.DispatchEpoch < 1 {
		http.Error(w, "invalid dispatch claim", 400)
		return
	}
	existing, err := h.taskStore.Get(r.Context(), requestNodeRunID(r))
	if err != nil || existing.RunID != requestRunID(r) {
		http.Error(w, "not found", 404)
		return
	}
	claimed, err := task.ClaimExecution(r.Context(), h.db, requestRunID(r), requestNodeRunID(r), body.NodeInstanceID, body.Owner, body.DispatchEpoch)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"claimed": claimed})
}

func (h *FlowRunHandler) DispatchTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var plan entities.ExecutionPlan
	if err := decodeStrictJSON(r, &plan); err != nil || plan.TaskID != requestNodeRunID(r) || plan.AgentFlowRunID != requestRunID(r) || plan.Namespace != r.PathValue("namespace") {
		http.Error(w, "invalid plan", 400)
		return
	}
	existing, err := h.taskStore.Get(r.Context(), plan.TaskID)
	if err != nil || existing.RunID != plan.AgentFlowRunID {
		http.Error(w, "not found", 404)
		return
	}
	if err = task.DispatchExecution(r.Context(), h.db, &plan); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(204)
}

// RunDefinition reads the locked revision, independent of subsequent edits.
func (h *FlowRunHandler) RunDefinition(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	var data []byte
	err := storage.WithSQLTransaction(r.Context(), h.db, func(tx *storage.SQLTransaction) error {
		return tx.QueryRow(r.Context(), `SELECT definition FROM orh_flow_revision WHERE id=? AND namespace_id=?`, run.FlowRevisionID, run.Namespace).Scan(&data)
	})
	if err != nil {
		http.Error(w, "definition unavailable", 500)
		return
	}
	var flow entities.FlowInfo
	if err = json.Unmarshal(data, &flow); err != nil {
		http.Error(w, "definition corrupt", 500)
		return
	}
	flow.Namespace = run.Namespace
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(flow)
}
