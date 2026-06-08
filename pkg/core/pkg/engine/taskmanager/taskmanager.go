package taskmanager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/model/pkg"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	sandbox "github.com/flowgent-labs/flowgent/cmd/pkg/sandbox"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TaskStateStore is the narrow state interface TM workers need for persistence.
// Implementations call the apiserver REST API (never direct DB).
type TaskStateStore interface {
	SaveTask(ctx context.Context, task *model.TaskRun) error
}

// TaskManagerConfig is the startup configuration for a TaskManager.
type TaskManagerConfig struct {
	ID                string
	SlotCount         int
	Queue             messager.IMessager
	State             TaskStateStore
	HumanApproval     executor.HumanApprovalStore
	Agents            []*config.AgentDef
	MCPClients        map[string]engine.MCPClient
	LLMClient         engine.LLMClient
	Logger            *utils.Logger
	HeartbeatInterval time.Duration
	SandboxQueue      messager.IMessager
	SandboxPolicy     *model.SandboxPolicy
	SandboxWorkspace  string
	SandboxDeploymentEnabled bool
}

// TaskManager is a persistent worker that consumes ExecutionPlans from
// a queue (MQTT or local) and executes them via a pool of SlotWorkers.
type TaskManager struct {
	ID          string
	slotWorkers []*SlotWorker
	router      *executor.TaskExecutorRouter
	queue       messager.IMessager
	state       TaskStateStore
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
	router.Register(executor.NewSupervisorExecutor(cfg.LLMClient, cfg.Agents))
	router.Register(&executor.TribunalExecutor{})
	router.Register(&executor.MapExecutor{})
	router.Register(&executor.JoinExecutor{})
	router.Register(&executor.SubflowExecutor{})
	router.Register(executor.NewHumanExecutor(cfg.HumanApproval))
	router.Register(&executor.NoopExecutor{})
	router.Register(&executor.SkillExecutor{})
	router.Register(executor.NewSandboxExecutor(cfg.SandboxQueue, cfg.SandboxPolicy, cfg.SandboxWorkspace))

	metrics := NewTaskManagerMetrics()

	tm := &TaskManager{
		ID:      cfg.ID,
		router:  router,
		queue:   cfg.Queue,
		state:   cfg.State,
		metrics: metrics,
		logger:  cfg.Logger,
		stopCh:  make(chan struct{}),
	}

	for i := 0; i < cfg.SlotCount; i++ {
		slotID := fmt.Sprintf("%s-slot-%d", cfg.ID, i)
		sw := NewSlotWorker(slotID, cfg.ID, cfg.Queue, router, cfg.State, metrics)
		tm.slotWorkers = append(tm.slotWorkers, sw)
	}

	if !cfg.SandboxDeploymentEnabled && cfg.SandboxQueue != nil {
		embeddedRunner := sandbox.NewSandboxRunner(
			cfg.ID+"-sb", cfg.SandboxQueue, "", cfg.SandboxWorkspace, cfg.SandboxPolicy)
		go func() {
			slog.Info("embedded sandbox runner started", "id", embeddedRunner.GetID())
			if err := embeddedRunner.Start(context.Background()); err != nil {
				slog.Error("embedded sandbox runner stopped", "error", err)
			}
		}()
	}

	return tm, nil
}

func (tm *TaskManager) Start(ctx context.Context) error {
	startHeartbeat(tm.ID, tm.queue, 0)
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
	_ = tm.state.SaveTask(ctx, task)
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
