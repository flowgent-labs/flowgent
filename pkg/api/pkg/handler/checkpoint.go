package handler

import (
	"encoding/json"
	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/noderun"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/nodepayload"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
)

func (h *GraphRunHandler) CreateCheckpoint(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	checkpoint, err := h.checkpoints.Create(r.Context(), run.Namespace, run.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(checkpoint)
}
func (h *GraphRunHandler) Recovery(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	recovery, err := h.checkpoints.Recover(r.Context(), run.Namespace, run.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Resolve through the same Artifact Provider used by ordinary NodeRun reads.
	for i := range recovery.NodeRuns {
		if err = h.resolveNodePayloads(r.Context(), &recovery.NodeRuns[i]); err != nil {
			h.payloadUnavailable(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recovery)
}
func (h *GraphRunHandler) CommitNodeCheckpoint(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var body struct {
		DispatchEpoch int64                   `json:"dispatch_epoch"`
		Checkpoint    entities.NodeCheckpoint `json:"checkpoint"`
	}
	if err := decodeStrictJSON(r, &body); err != nil {
		http.Error(w, "invalid checkpoint", 400)
		return
	}
	node, err := h.taskStore.Get(r.Context(), requestNodeRunID(r))
	if err != nil || node.GraphRunID != requestGraphRunID(r) {
		http.Error(w, "not found", 404)
		return
	}
	if node.Status != entities.NodeRunRunning || node.DispatchEpoch != body.DispatchEpoch {
		http.Error(w, "checkpoint execution identity conflict", 409)
		return
	}
	data, err := json.Marshal(body.Checkpoint)
	if err != nil {
		http.Error(w, "invalid checkpoint", 400)
		return
	}
	if err = json.Unmarshal(data, &node.Checkpoint); err != nil {
		http.Error(w, "invalid checkpoint", 400)
		return
	}
	if body.Checkpoint.WorkspaceSnapshot != nil {
		node.WorkspaceVersion = body.Checkpoint.WorkspaceSnapshot.ID
	}
	key := nodepayload.NodePayloadKey{Namespace: node.Namespace, GraphRunID: node.GraphRunID, NodeRunID: node.ID, Kind: nodepayload.PayloadCheckpoint}
	stored, err := h.payloads.Persist(r.Context(), key, node.Checkpoint)
	if err != nil {
		h.payloadUnavailable(w, err)
		return
	}
	node.Checkpoint = stored
	if err = h.taskStore.UpdateNodeRun(r.Context(), node); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(204)
}

// AgentSnapshot resolves only an agent revision pinned to this GraphRun.
func (h *GraphRunHandler) AgentSnapshot(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	name := r.PathValue("agent_name")
	agents, _ := run.ContextSnapshot["agent_revisions"].(map[string]any)
	locked, _ := agents[name].(map[string]any)
	revisionID, _ := locked["revision_id"].(string)
	if revisionID == "" {
		http.Error(w, "agent not pinned to GraphRun", 404)
		return
	}
	agent := entities.AgentInfo{BaseEntity: entities.BaseEntity{Namespace: run.Namespace}, Name: name}
	var model, input, output []byte
	err := storage.WithSQLTransaction(r.Context(), h.db, func(tx *storage.SQLTransaction) error {
		return tx.QueryRow(r.Context(), `SELECT agent_id,revision,soul,instruction,model_config,input_schema,output_schema FROM llm_agent_revision WHERE id=? AND namespace_id=?`, revisionID, run.Namespace).Scan(&agent.ID, &agent.Revision, &agent.Soul, &agent.Instruction, &model, &input, &output)
	})
	if err != nil {
		http.Error(w, "pinned agent unavailable", 500)
		return
	}
	if err = json.Unmarshal(model, &agent); err != nil {
		http.Error(w, "invalid pinned model config", 500)
		return
	}
	_ = json.Unmarshal(input, &agent.InputSchema)
	_ = json.Unmarshal(output, &agent.OutputSchema)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

func (h *GraphRunHandler) DispatchSandbox(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var trigger model.SandboxTrigger
	if err := decodeStrictJSON(r, &trigger); err != nil || trigger.GraphRunID != requestGraphRunID(r) || trigger.NodeRunID != requestNodeRunID(r) || trigger.Namespace != r.PathValue("namespace") {
		http.Error(w, "invalid sandbox dispatch", 400)
		return
	}
	node, err := h.taskStore.Get(r.Context(), trigger.NodeRunID)
	if err != nil || node.GraphRunID != trigger.GraphRunID {
		http.Error(w, "not found", 404)
		return
	}
	if err = noderun.DispatchSandbox(r.Context(), h.db, &trigger); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(204)
}
func (h *GraphRunHandler) CommitSandboxResult(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", 404)
		return
	}
	var result entities.NodeResult
	if err := decodeStrictJSON(r, &result); err != nil || result.NodeRunID != requestNodeRunID(r) {
		http.Error(w, "invalid sandbox result", 400)
		return
	}
	node, err := h.taskStore.Get(r.Context(), result.NodeRunID)
	if err != nil || node.GraphRunID != requestGraphRunID(r) {
		http.Error(w, "not found", 404)
		return
	}
	if node.DispatchEpoch != result.DispatchEpoch {
		http.Error(w, "execution identity conflict", 409)
		return
	}
	if result.Error == "" && result.WorkspaceSnapshot == nil {
		http.Error(w, "verified workspace snapshot is required", 422)
		return
	}
	if err = h.resolveNodePayloads(r.Context(), node); err != nil {
		h.payloadUnavailable(w, err)
		return
	}
	node.Output = result.Output
	node.Error = result.Error
	node.WorkspaceSnapshot = result.WorkspaceSnapshot
	node.Status = entities.NodeRunSucceeded
	if result.Error != "" {
		node.Status = entities.NodeRunFailed
	}
	if result.WorkspaceSnapshot != nil {
		node.WorkspaceVersion = result.WorkspaceSnapshot.ID
	}
	now := time.Now().UTC()
	node.FinishedAt = &now
	if result.Checkpoint != nil {
		data, _ := json.Marshal(result.Checkpoint)
		_ = json.Unmarshal(data, &node.Checkpoint)
	}
	if err = h.persistNodePayloads(r.Context(), node); err != nil {
		h.payloadUnavailable(w, err)
		return
	}
	if err = h.taskStore.UpdateNodeRun(r.Context(), node); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(204)
}
