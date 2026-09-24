package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/taskpayload"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowRunHandler struct {
	runStore  flowrun.IFlowRunStore
	taskStore task.ITaskStore
	payloads  taskpayload.ITaskPayloadProvider
	mqtt      MQTTPublisher
	logger    *utils.Logger
}

func NewFlowRunHandler(s storage.IStorage, payloads taskpayload.ITaskPayloadProvider, mqtt MQTTPublisher, logger *utils.Logger) *FlowRunHandler {
	var runStore flowrun.IFlowRunStore
	var taskStore task.ITaskStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		runStore = flowrun.NewFlowRunPostgresStore(db)
		taskStore = task.NewTaskPostgresStore(db)
	case *sql.DB:
		runStore = flowrun.NewFlowRunSQLiteStore(db)
		taskStore = task.NewTaskSQLiteStore(db)
	}
	if payloads == nil {
		payloads = taskpayload.NewDefaultTaskPayloadProvider(0)
	}
	return &FlowRunHandler{runStore: runStore, taskStore: taskStore, payloads: payloads, mqtt: mqtt, logger: logger}
}

// Create inserts a new FlowRunInfo.
func (h *FlowRunHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var run entities.FlowRunInfo
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
// for a directly-created run (POST /runs — used by the Controller's
// createScheduledRun and by tests). Mirrors FlowDefHandler's event so both
// run-creation paths publish the identical topic/payload (docs §2.4 /
// VERIFICATION.md §4.2.4). Best-effort — a broker outage never fails the
// create.
func (h *FlowRunHandler) publishRunCreatedEvent(ctx context.Context, run *entities.FlowRunInfo) {
	if h.mqtt == nil {
		return
	}
	topic := fmt.Sprintf("flowgent/v1/%s/flows/%s/runs/%s/ctrl/run/created", run.Namespace, run.AgentFlowID, run.ID)
	payload, _ := json.Marshal(map[string]any{
		"event_type":   "CREATED",
		"run_id":       run.ID,
		"flow_id":      run.FlowName,
		"namespace_id": run.Namespace,
		"namespace":    run.K8sNamespace,
		"status":       run.Status,
	})
	if err := h.mqtt.Publish(ctx, topic, payload); err != nil {
		h.logger.Warn("mqtt run created event publish failed", "topic", topic, "error", err)
	}
}

// List returns runs with optional query-param filters.
func (h *FlowRunHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 {
		size = 50
	}
	flowID := r.PathValue("flow_id")
	if flowID == "" {
		flowID = q.Get("flow_id")
	}
	runs, err := h.runStore.List(r.Context(), flowrun.ListFilter{
		Namespace:    r.PathValue("namespace"),
		Status:       q.Get("status"),
		RuntimeMode:  q.Get("runtime_mode"),
		K8sNamespace: q.Get("k8s_namespace"),
		FlowID:       flowID,
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
func (h *FlowRunHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	hours := queryInt(r, "hours", 24, 1, 24*30)
	buckets := queryInt(r, "buckets", min(hours, 12), 1, 120)
	until := time.Now().UTC()
	metrics, err := h.runStore.Metrics(r.Context(), flowrun.MetricRequest{
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

func (h *FlowRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	run, ok := h.ownedRun(r)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// Update persists the narrow lifecycle transition produced by JobMaster.
func (h *FlowRunHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req entities.RunLifecycleUpdate
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

func (h *FlowRunHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tasks, _ := h.taskStore.ListByFlowRun(r.Context(), requestRunID(r))
	if err := h.runStore.Delete(r.Context(), requestRunID(r)); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	for _, task := range tasks {
		h.deletePayloadBestEffort(r.Context(), task.Input)
		h.deletePayloadBestEffort(r.Context(), task.Output)
	}
	w.WriteHeader(200)
}

func (h *FlowRunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.runStore.Cancel(r.Context(), requestRunID(r)); err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.WriteHeader(200)
}

func (h *FlowRunHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tasks, err := h.taskStore.ListByFlowRun(r.Context(), requestRunID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, task := range tasks {
		if err := h.resolveTaskPayloads(r.Context(), task); err != nil {
			h.payloadUnavailable(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// CreateTask creates a new task run for a flow run.
func (h *FlowRunHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var task entities.TaskRunInfo
	if err := decodeStrictJSON(r, &task); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	task.RunID = requestRunID(r)
	task.AgentFlowRunID = task.RunID
	task.Namespace = r.PathValue("namespace")
	task.NormalizeAliases()
	if task.ID != "" {
		if existing, err := h.taskStore.Get(r.Context(), task.ID); err == nil && existing != nil {
			http.Error(w, "task already exists", http.StatusConflict)
			return
		} else if err != nil && !isNotFoundError(err) {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
	} else {
		task.ID = uuid.NewString()
	}
	originalInput, originalOutput := task.Input, task.Output
	if err := h.persistTaskPayloads(r.Context(), &task); err != nil {
		h.cleanupReplacedPayloads(r.Context(), nil, &task)
		h.payloadUnavailable(w, err)
		return
	}
	if err := h.taskStore.CreateTaskRun(r.Context(), &task); err != nil {
		h.deletePayloadBestEffort(r.Context(), task.Input)
		h.deletePayloadBestEffort(r.Context(), task.Output)
		http.Error(w, err.Error(), 500)
		return
	}
	task.Input, task.Output = originalInput, originalOutput
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(task)
}

func (h *FlowRunHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	task, err := h.taskStore.Get(r.Context(), requestNodeRunID(r))
	if task != nil {
		task.NormalizeAliases()
	}
	if err != nil || task == nil || task.RunID != requestRunID(r) {
		http.Error(w, "not found", 404)
		return
	}
	if err := h.resolveTaskPayloads(r.Context(), task); err != nil {
		h.payloadUnavailable(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (h *FlowRunHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ownedRun(r); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var task entities.TaskRunInfo
	if err := decodeStrictJSON(r, &task); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	task.ID = requestNodeRunID(r)
	task.RunID = requestRunID(r)
	task.AgentFlowRunID = task.RunID
	task.Namespace = r.PathValue("namespace")
	task.NormalizeAliases()
	var existing *entities.TaskRunInfo
	if current, err := h.taskStore.Get(r.Context(), task.ID); err == nil && current != nil {
		existing = current
		existing.NormalizeAliases()
		if existing.RunID != task.RunID {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	} else if err != nil && !isNotFoundError(err) {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		if task.Input == nil {
			task.Input = existing.Input
		}
		if task.Output == nil {
			task.Output = existing.Output
		}
	}
	if err := h.persistTaskPayloads(r.Context(), &task); err != nil {
		h.cleanupReplacedPayloads(r.Context(), existing, &task)
		h.payloadUnavailable(w, err)
		return
	}
	if err := h.taskStore.UpdateTaskRun(r.Context(), &task); err != nil {
		h.cleanupReplacedPayloads(r.Context(), existing, &task)
		h.logger.Error("save task", "error", err)
		http.Error(w, "internal", 500)
		return
	}
	if existing != nil {
		if !reflect.DeepEqual(existing.Input, task.Input) {
			h.deletePayloadBestEffort(r.Context(), existing.Input)
		}
		if !reflect.DeepEqual(existing.Output, task.Output) {
			h.deletePayloadBestEffort(r.Context(), existing.Output)
		}
	}
	w.WriteHeader(200)
}

func (h *FlowRunHandler) persistTaskPayloads(ctx context.Context, task *entities.TaskRunInfo) error {
	key := taskpayload.TaskPayloadKey{Namespace: task.Namespace, RunID: task.RunID, TaskID: task.ID}
	key.Kind = taskpayload.PayloadInput
	input, err := h.payloads.Persist(ctx, key, task.Input)
	if err != nil {
		return fmt.Errorf("persist task input: %w", err)
	}
	task.Input = input
	key.Kind = taskpayload.PayloadOutput
	output, err := h.payloads.Persist(ctx, key, task.Output)
	if err != nil {
		return fmt.Errorf("persist task output: %w", err)
	}
	task.Output = output
	return nil
}

func (h *FlowRunHandler) resolveTaskPayloads(ctx context.Context, task *entities.TaskRunInfo) error {
	input, err := h.payloads.Resolve(ctx, task.Input)
	if err != nil {
		return fmt.Errorf("resolve task input: %w", err)
	}
	output, err := h.payloads.Resolve(ctx, task.Output)
	if err != nil {
		return fmt.Errorf("resolve task output: %w", err)
	}
	task.Input, task.Output = input, output
	return nil
}

// cleanupReplacedPayloads removes only newly-created objects after a failed DB
// write; objects already referenced by the current row must remain intact.
func (h *FlowRunHandler) cleanupReplacedPayloads(ctx context.Context, existing, next *entities.TaskRunInfo) {
	if existing == nil || !reflect.DeepEqual(existing.Input, next.Input) {
		h.deletePayloadBestEffort(ctx, next.Input)
	}
	if existing == nil || !reflect.DeepEqual(existing.Output, next.Output) {
		h.deletePayloadBestEffort(ctx, next.Output)
	}
}

func (h *FlowRunHandler) deletePayloadBestEffort(ctx context.Context, stored map[string]any) {
	if err := h.payloads.Delete(ctx, stored); err != nil && h.logger != nil {
		h.logger.Warn("delete task payload", "provider", h.payloads.Name(), "error", err)
	}
}

func (h *FlowRunHandler) payloadUnavailable(w http.ResponseWriter, err error) {
	if h.logger != nil {
		h.logger.Error("task payload unavailable", "provider", h.payloads.Name(), "error", err)
	}
	http.Error(w, "task payload unavailable", http.StatusBadGateway)
}

func (h *FlowRunHandler) ownedRun(r *http.Request) (*entities.FlowRunInfo, bool) {
	run, err := h.runStore.Get(r.Context(), requestRunID(r))
	if err != nil || run == nil || run.Namespace != r.PathValue("namespace") {
		return nil, false
	}
	run.NormalizeAliases()
	if flowID := r.PathValue("flow_id"); flowID != "" && run.FlowName != flowID && run.AgentFlowID != flowID {
		return nil, false
	}
	return run, true
}

func requestRunID(r *http.Request) string {
	return r.PathValue("run_id")
}

func requestNodeRunID(r *http.Request) string {
	return r.PathValue("node_run_id")
}
