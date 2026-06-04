package taskmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/src/tracing"
	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/config/src"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/core/src/engine/executor"
	"github.com/flowgent-labs/flowgent/model/src"
	messaging "github.com/flowgent-labs/flowgent/messaging/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TaskManagerConfig is the startup configuration for a TaskManager.
type TaskManagerConfig struct {
	ID                string
	SlotCount         int
	Queue             messaging.IMessager
	Store             store.IStore
	Agents            []*config.AgentDef
	MCPClients        map[string]engine.MCPClient
	LLMClient         engine.LLMClient
	Logger            *utils.Logger
	HeartbeatInterval time.Duration
	SandboxQueue      messaging.IMessager
	SandboxPolicy     *model.SandboxPolicy
	SandboxWorkspace  string
}

// TaskManager is a persistent worker that consumes ExecutionPlans from
// a queue (MQTT or local) and executes them via a pool of SlotWorkers.
// Designed as a long-lived K8s Deployment pod.
type TaskManager struct {
	ID          string
	slotWorkers []*SlotWorker
	router      *executor.TaskExecutorRouter
	queue       messaging.IMessager
	store       store.IStore
	metrics     *TaskManagerMetrics
	logger      *utils.Logger
	mu          sync.Mutex
	stopCh      chan struct{}
	stopped     bool
}

func NewTaskManager(cfg *TaskManagerConfig) (*TaskManager, error) {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("tm-%d", time.Now().UnixNano())
	}
	if cfg.SlotCount <= 0 {
		cfg.SlotCount = 4
	}

	router := executor.NewTaskExecutorRouter()
	router.Register(executor.NewAgentExecutor(cfg.LLMClient, cfg.Agents))
	router.Register(&executor.ConditionExecutor{})
	router.Register(executor.NewToolExecutor(cfg.MCPClients))
	router.Register(executor.NewSupervisorExecutor(cfg.LLMClient, cfg.Agents, cfg.Store))
	router.Register(&executor.TribunalExecutor{})
	router.Register(&executor.MapExecutor{})
	router.Register(&executor.JoinExecutor{})
	router.Register(&executor.SubflowExecutor{})
	router.Register(executor.NewHumanExecutor(cfg.Store))
	router.Register(&executor.NoopExecutor{})
	router.Register(&executor.SkillExecutor{})
	router.Register(executor.NewSandboxExecutor(cfg.SandboxQueue, cfg.SandboxPolicy, cfg.SandboxWorkspace))

	metrics := NewTaskManagerMetrics()

	tm := &TaskManager{
		ID:      cfg.ID,
		router:  router,
		queue:   cfg.Queue,
		store:   cfg.Store,
		metrics: metrics,
		logger:  cfg.Logger,
		stopCh:  make(chan struct{}),
	}

	for i := 0; i < cfg.SlotCount; i++ {
		slotID := fmt.Sprintf("%s-slot-%d", cfg.ID, i)
		sw := NewSlotWorker(slotID, cfg.ID, cfg.Queue, router, cfg.Store, metrics)
		tm.slotWorkers = append(tm.slotWorkers, sw)
	}
	return tm, nil
}

func (tm *TaskManager) Start(ctx context.Context) error {
	// Start heartbeat pump (TM liveness)
	startHeartbeat(tm.ID, tm.queue, 0)
	// Start all slot workers
	for _, sw := range tm.slotWorkers {
		go sw.Loop(ctx)
	}
	tm.logger.Info("task manager started", "id", tm.ID, "slots", len(tm.slotWorkers))
	return nil
}

func (tm *TaskManager) Stop() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if !tm.stopped {
		tm.stopped = true
		close(tm.stopCh)
	}
}

// ExecutePlan executes a single ExecutionPlan via the router.
func (tm *TaskManager) ExecutePlan(ctx context.Context, plan *model.ExecutionPlan, task *model.TaskRun) (*model.TaskResult, error) {
	task.Input = plan.Input
	scope := map[string]map[string]any{"input": plan.Input}
	result, err := tm.router.Execute(ctx, plan, scope)
	if err != nil {
		return nil, err
	}
	task.Output = result.Output
	task.Status = model.Success
	now := time.Now()
	task.FinishedAt = &now
	plan.FinishedAt = &now
	_ = tm.store.UpdateTaskRun(ctx, task)
	return result, nil
}

// ─── Metrics ────────────────────────────────────────────

type TaskManagerMetrics struct {
	meter          metric.Meter
	SlotsBusy      metric.Int64UpDownCounter
	TasksExecuted  metric.Int64Counter
	TaskDuration   metric.Float64Histogram
	DequeueLatency metric.Float64Histogram
}

func NewTaskManagerMetrics() *TaskManagerMetrics {
	m := &TaskManagerMetrics{meter: tracing.Meter("flowgent/taskmanager")}
	m.SlotsBusy, _ = m.meter.Int64UpDownCounter("flowgent.tm.slots.busy",
		metric.WithDescription("Currently occupied slots"))
	m.TasksExecuted, _ = m.meter.Int64Counter("flowgent.tm.tasks.executed",
		metric.WithDescription("Total tasks executed"))
	m.TaskDuration, _ = m.meter.Float64Histogram("flowgent.tm.tasks.duration",
		metric.WithDescription("Task wall-clock time (ms)"),
		metric.WithExplicitBucketBoundaries(10, 50, 100, 500, 1000, 5000, 30000, 60000))
	m.DequeueLatency, _ = m.meter.Float64Histogram("flowgent.tm.dequeue.latency",
		metric.WithDescription("MQTT dequeue→acquire time (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000))
	return m
}

func taskTypeAttr(t model.TaskType) attribute.KeyValue {
	return attribute.String("task_type", string(t))
}
