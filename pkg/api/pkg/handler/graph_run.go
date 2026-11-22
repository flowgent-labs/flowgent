package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/nodepayload"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	taskstore "github.com/flowgent-labs/flowgent/storage/pkg/noderun"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GraphRunHandler struct {
	db          any
	checkpoints *graphrun.CheckpointStore
	subgraphs   *graphrun.SubGraphStore
	runStore    graphrun.IGraphRunStore
	taskStore   taskstore.INodeRunStore
	payloads    nodepayload.INodePayloadProvider
	mqtt        MQTTPublisher
	logger      *utils.Logger
}

func NewGraphRunHandler(s storage.IStorage, payloads nodepayload.INodePayloadProvider, mqtt MQTTPublisher, logger *utils.Logger) *GraphRunHandler {
	var runStore graphrun.IGraphRunStore
	var taskStore taskstore.INodeRunStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		runStore = graphrun.NewGraphRunPostgresStore(db)
		taskStore = taskstore.NewNodeRunPostgresStore(db)
	case *sql.DB:
		runStore = graphrun.NewGraphRunSQLiteStore(db)
		taskStore = taskstore.NewNodeRunSQLiteStore(db)
	}
	if payloads == nil {
		payloads = nodepayload.NewDefaultNodePayloadProvider(0)
	}
	return &GraphRunHandler{db: s.DB(), checkpoints: graphrun.NewCheckpointStore(s.DB()), subgraphs: graphrun.NewSubGraphStore(s.DB()), runStore: runStore, taskStore: taskStore, payloads: payloads, mqtt: mqtt, logger: logger}
}

// Create inserts a new GraphRun.
func (h *GraphRunHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var run entities.GraphRun
	if err := decodeStrictJSON(r, &run); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if run.Namespace != "" && run.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	run.Namespace = namespace
	if err := entities.ValidateRuntimeMode(run.RuntimeMode); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.runStore.Create(r.Context(), &run); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	h.publishRunCreatedEvent(r.Context(), &run)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(run)
}

// publishRunCreatedEvent emits the `ctrl/run/created` lifecycle event on MQTT
// for a directly-created run (POST /graph-runs — used by the Controller's
// createScheduledRun and by tests). Mirrors AgentGraphHandler's event so both
// run-creation paths publish the identical topic/payload (docs §2.4 /
// VERIFICATION.md §4.2.4). Best-effort — a broker outage never fails the
// create.
func (h *GraphRunHandler) publishRunCreatedEvent(ctx context.Context, run *entities.GraphRun) {
	if h.mqtt == nil {
		return
	}
	topic := fmt.Sprintf("flowgent/v1/%s/agent-graphs/%s/graph-runs/%s/ctrl/graph-run/created", run.Namespace, run.AgentGraphID, run.ID)
	payload, _ := json.Marshal(map[string]any{
		"event_type":       "CREATED",
		"graph_run_id":     run.ID,
		"agent_graph_id":   run.AgentGraphID,
		"agent_graph_name": run.AgentGraphName,
		"namespace_id":     run.Namespace,
		"namespace":        run.K8sNamespace,
		"status":           run.Status,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		h.logger.Warn("mqtt run created event publish failed", "topic", topic, "error", err)
	}
}

// List returns runs with optional query-param filters.
func (h *GraphRunHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 {
		size = 50
	}
	graphID := r.PathValue("agent_graph_id")
	if graphID == "" {
		graphID = q.Get("agent_graph_id")
	}
	runs, err := h.runStore.List(r.Context(), graphrun.ListFilter{
		Namespace:    r.PathValue("namespace"),
		Status:       q.Get("status"),
		RuntimeMode:  q.Get("runtime_mode"),
		K8sNamespace: q.Get("k8s_namespace"),
		AgentGraphID: graphID,
		Page:         entities.PageRequest{Page: page, Size: size},
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// Metrics aggregates the namespace's complete run population in the database;
// the dashboard never derives operational totals from a truncated list page.
func (h *GraphRunHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	hours := queryInt(r, "hours", 24, 1, 24*30)
	buckets := queryInt(r, "buckets", min(hours, 12), 1, 120)
	until := time.Now().UTC()
	metrics, err := h.runStore.Metrics(r.Context(), graphrun.MetricRequest{
		Namespace: r.PathValue("namespace"),
		Since:     until.Add(-time.Duration(hours) * time.Hour),
		Until:     until,
		Buckets:   buckets,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func queryInt(r *http.Request, name string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}

func (h *GraphRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	nodes, err := h.taskStore.ListByGraphRun(r.Context(), run.ID)
	if err != nil {
		http.Error(w, "node runs unavailable", 500)
		return
	}
	for _, node := range nodes {
		if err := h.resolveNodePayloads(r.Context(), node); err != nil {
			h.payloadUnavailable(w, err)
			return
		}
		run.NodeRuns = append(run.NodeRuns, *node)
	}
	json.NewEncoder(w).Encode(run)
}

// Update persists the narrow lifecycle transition produced by JobMaster.
func (h *GraphRunHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req entities.GraphRunLifecycleUpdate
	if err := decodeStrictJSON(r, &req); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	if req.Status != "" {
		run.Status = req.Status
		if req.Status == entities.GraphRunPending || req.Status == entities.GraphRunRunning || req.Status == entities.GraphRunPaused {
			run.FinishedAt = nil
		}
	}
	if req.Error != "" {
		run.Error = req.Error
	}
	if req.StartedAt != nil {
		run.StartedAt = req.StartedAt
	}
	if req.FinishedAt != nil {
		run.FinishedAt = req.FinishedAt
	}
	if run.FinishedAt != nil && run.StartedAt != nil && run.FinishedAt.Before(*run.StartedAt) {
		http.Error(w, "finished_at must not precede started_at", http.StatusBadRequest)
		return
	}
	if err := h.runStore.Update(r.Context(), run); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}

func (h *GraphRunHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tasks, _ := h.taskStore.ListByGraphRun(r.Context(), requestGraphRunID(r))
	if err := h.runStore.Delete(r.Context(), requestGraphRunID(r)); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	for _, noderun := range tasks {
		h.deletePayloadBestEffort(r.Context(), noderun.Input)
		h.deletePayloadBestEffort(r.Context(), noderun.Output)
	}
	w.WriteHeader(200)
}

func (h *GraphRunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.runStore.Cancel(r.Context(), requestGraphRunID(r)); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}

func (h *GraphRunHandler) ListNodeRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tasks, err := h.taskStore.ListByGraphRun(r.Context(), requestGraphRunID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, noderun := range tasks {
		if err := h.resolveNodePayloads(r.Context(), noderun); err != nil {
			h.payloadUnavailable(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// CreateNodeRun creates a new noderun run for a graph run.
func (h *GraphRunHandler) CreateNodeRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var noderun entities.NodeRun
	if err := decodeStrictJSON(r, &noderun); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	noderun.GraphRunID = requestGraphRunID(r)
	noderun.GraphRunID = noderun.GraphRunID
	noderun.Namespace = r.PathValue("namespace")
	noderun.Normalize()
	if noderun.ID != "" {
		if existing, err := h.taskStore.Get(r.Context(), noderun.ID); err == nil && existing != nil {
			http.Error(w, "noderun already exists", http.StatusConflict)
			return
		} else if err != nil && !isNotFoundError(err) {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
	} else {
		noderun.ID = uuid.NewString()
	}
	originalInput, originalOutput := noderun.Input, noderun.Output
	if err := h.persistNodePayloads(r.Context(), &noderun); err != nil {
		h.cleanupReplacedPayloads(r.Context(), nil, &noderun)
		h.payloadUnavailable(w, err)
		return
	}
	if err := h.taskStore.CreateNodeRun(r.Context(), &noderun); err != nil {
		h.deletePayloadBestEffort(r.Context(), noderun.Input)
		h.deletePayloadBestEffort(r.Context(), noderun.Output)
		http.Error(w, err.Error(), 500)
		return
	}
	noderun.Input, noderun.Output = originalInput, originalOutput
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(noderun)
}

func (h *GraphRunHandler) GetNodeRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	noderun, err := h.taskStore.Get(r.Context(), requestNodeRunID(r))
	if noderun != nil {
		noderun.Normalize()
	}
	if err != nil || noderun == nil || noderun.GraphRunID != requestGraphRunID(r) {
		http.Error(w, "not found", 404)
		return
	}
	if err := h.resolveNodePayloads(r.Context(), noderun); err != nil {
		h.payloadUnavailable(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(noderun)
}

func (h *GraphRunHandler) UpdateNodeRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var noderun entities.NodeRun
	if err := decodeStrictJSON(r, &noderun); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	noderun.ID = requestNodeRunID(r)
	noderun.GraphRunID = requestGraphRunID(r)
	noderun.GraphRunID = noderun.GraphRunID
	noderun.Namespace = r.PathValue("namespace")
	noderun.Normalize()
	var existing *entities.NodeRun
	if current, err := h.taskStore.Get(r.Context(), noderun.ID); err == nil && current != nil {
		existing = current
		existing.Normalize()
		if existing.GraphRunID != noderun.GraphRunID {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	} else if err != nil && !isNotFoundError(err) {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		if noderun.Input == nil {
			noderun.Input = existing.Input
		}
		if noderun.Output == nil {
			noderun.Output = existing.Output
		}
	}
	if err := h.persistNodePayloads(r.Context(), &noderun); err != nil {
		h.cleanupReplacedPayloads(r.Context(), existing, &noderun)
		h.payloadUnavailable(w, err)
		return
	}
	if err := h.taskStore.UpdateNodeRun(r.Context(), &noderun); err != nil {
		h.cleanupReplacedPayloads(r.Context(), existing, &noderun)
		if errors.Is(err, taskstore.ErrExecutionConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		h.logger.Error("save noderun", "error", err)
		http.Error(w, "internal", 500)
		return
	}
	if existing != nil {
		if !reflect.DeepEqual(existing.Input, noderun.Input) {
			h.deletePayloadBestEffort(r.Context(), existing.Input)
		}
		if !reflect.DeepEqual(existing.Output, noderun.Output) {
			h.deletePayloadBestEffort(r.Context(), existing.Output)
		}
	}
	w.WriteHeader(200)
}

func (h *GraphRunHandler) persistNodePayloads(ctx context.Context, noderun *entities.NodeRun) error {
	key := nodepayload.NodePayloadKey{Namespace: noderun.Namespace, GraphRunID: noderun.GraphRunID, NodeRunID: noderun.ID}
	key.Kind = nodepayload.PayloadInput
	input, err := h.payloads.Persist(ctx, key, noderun.Input)
	if err != nil {
		return fmt.Errorf("persist noderun input: %w", err)
	}
	noderun.Input = input
	key.Kind = nodepayload.PayloadOutput
	output, err := h.payloads.Persist(ctx, key, noderun.Output)
	if err != nil {
		return fmt.Errorf("persist noderun output: %w", err)
	}
	noderun.Output = output
	key.Kind = nodepayload.PayloadCheckpoint
	checkpoint, err := h.payloads.Persist(ctx, key, noderun.Checkpoint)
	if err != nil {
		return fmt.Errorf("persist node checkpoint: %w", err)
	}
	noderun.Checkpoint = checkpoint
	key.Kind = nodepayload.PayloadContext
	memory, err := h.payloads.Persist(ctx, key, noderun.ExecutionMemory)
	if err != nil {
		return fmt.Errorf("persist node context: %w", err)
	}
	noderun.ExecutionMemory = memory
	return nil
}

func (h *GraphRunHandler) resolveNodePayloads(ctx context.Context, noderun *entities.NodeRun) error {
	input, err := h.payloads.Resolve(ctx, noderun.Input)
	if err != nil {
		return fmt.Errorf("resolve noderun input: %w", err)
	}
	output, err := h.payloads.Resolve(ctx, noderun.Output)
	if err != nil {
		return fmt.Errorf("resolve noderun output: %w", err)
	}
	checkpoint, err := h.payloads.Resolve(ctx, noderun.Checkpoint)
	if err != nil {
		return fmt.Errorf("resolve node checkpoint: %w", err)
	}
	memory, err := h.payloads.Resolve(ctx, noderun.ExecutionMemory)
	if err != nil {
		return fmt.Errorf("resolve node context: %w", err)
	}
	noderun.Input, noderun.Output, noderun.Checkpoint, noderun.ExecutionMemory = input, output, checkpoint, memory
	return nil
}

// cleanupReplacedPayloads removes only newly-created objects after a failed DB
// write; objects already referenced by the current row must remain intact.
func (h *GraphRunHandler) cleanupReplacedPayloads(ctx context.Context, existing, next *entities.NodeRun) {
	if existing == nil || !reflect.DeepEqual(existing.Input, next.Input) {
		h.deletePayloadBestEffort(ctx, next.Input)
	}
	if existing == nil || !reflect.DeepEqual(existing.Output, next.Output) {
		h.deletePayloadBestEffort(ctx, next.Output)
	}
	if existing == nil || !reflect.DeepEqual(existing.Checkpoint, next.Checkpoint) {
		h.deletePayloadBestEffort(ctx, next.Checkpoint)
	}
	if existing == nil || !reflect.DeepEqual(existing.ExecutionMemory, next.ExecutionMemory) {
		h.deletePayloadBestEffort(ctx, next.ExecutionMemory)
	}
}

func (h *GraphRunHandler) deletePayloadBestEffort(ctx context.Context, stored map[string]any) {
	if err := h.payloads.Delete(ctx, stored); err != nil && h.logger != nil {
		h.logger.Warn("delete noderun payload", "provider", h.payloads.Name(), "error", err)
	}
}

func (h *GraphRunHandler) payloadUnavailable(w http.ResponseWriter, err error) {
	if h.logger != nil {
		h.logger.Error("noderun payload unavailable", "provider", h.payloads.Name(), "error", err)
	}
	http.Error(w, "noderun payload unavailable", http.StatusBadGateway)
}

func (h *GraphRunHandler) ownedRun(r *http.Request) (*entities.GraphRun, bool) {
	run, err := h.runStore.Get(r.Context(), requestGraphRunID(r))
	if err != nil || run == nil || run.Namespace != r.PathValue("namespace") {
		return nil, false
	}
	run.Normalize()
	if graphID := r.PathValue("agent_graph_id"); graphID != "" && run.AgentGraphName != graphID && run.AgentGraphID != graphID {
		return nil, false
	}
	return run, true
}

func requestGraphRunID(r *http.Request) string {
	return r.PathValue("graph_run_id")
}

func requestNodeRunID(r *http.Request) string {
	return r.PathValue("node_run_id")
}
