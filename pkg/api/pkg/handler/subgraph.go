package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	noderun "github.com/flowgent-labs/flowgent/storage/pkg/noderun"
)

func (h *GraphRunHandler) InitializeSubGraph(w http.ResponseWriter, r *http.Request) {
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
	state, err := h.subgraphs.Transact(r.Context(), r.PathValue("namespace"), requestGraphRunID(r), body.InstanceID, nil)
	h.subgraphResponse(w, state, err)
}
func (h *GraphRunHandler) ApplyGraphPatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var patch entities.GraphPatch
	if err := decodeStrictJSON(r, &patch); err != nil || patch.GraphRunID != requestGraphRunID(r) {
		http.Error(w, "invalid patch", 400)
		return
	}
	state, err := h.subgraphs.Transact(r.Context(), r.PathValue("namespace"), requestGraphRunID(r), patch.SubGraphInstanceID, &patch)
	h.subgraphResponse(w, state, err)
}
func (h *GraphRunHandler) subgraphResponse(w http.ResponseWriter, state *entities.SubGraph, err error) {
	if err != nil {
		code := http.StatusUnprocessableEntity
		if errors.Is(err, graphrun.ErrGraphConflict) || strings.Contains(err.Error(), "defer_patch") {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// ClaimNodeRun consumes the durable dispatch identity before a TM invokes tools.
func (h *GraphRunHandler) ClaimNodeRun(w http.ResponseWriter, r *http.Request) {
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
	if err != nil || existing.GraphRunID != requestGraphRunID(r) {
		http.Error(w, "not found", 404)
		return
	}
	claimed, err := noderun.ClaimExecution(r.Context(), h.db, requestGraphRunID(r), requestNodeRunID(r), body.NodeInstanceID, body.Owner, body.DispatchEpoch)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"claimed": claimed})
}

func (h *GraphRunHandler) DispatchNodeRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var plan entities.ExecutionPlan
	if err := decodeStrictJSON(r, &plan); err != nil || plan.NodeRunID != requestNodeRunID(r) || plan.GraphRunID != requestGraphRunID(r) || plan.Namespace != r.PathValue("namespace") {
		http.Error(w, "invalid plan", 400)
		return
	}
	existing, err := h.taskStore.Get(r.Context(), plan.NodeRunID)
	if err != nil || existing.GraphRunID != plan.GraphRunID {
		http.Error(w, "not found", 404)
		return
	}
	if err = noderun.DispatchExecution(r.Context(), h.db, &plan); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(204)
}

// GraphSnapshot reads the locked revision, independent of subsequent edits.
func (h *GraphRunHandler) GraphSnapshot(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run.Snapshot)
}
