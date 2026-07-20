package taskmanager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/core/pkg/llm"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	sandbox "github.com/flowgent-labs/flowgent/sandbox/pkg"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TaskStateStore is the narrow state interface TM workers need for persistence.
// Implementations call the apiserver REST API (never direct DB).
type TaskStateStore interface {
	SaveTask(ctx context.Context, task *entities.TaskRunInfo) error
}

// TaskManagerConfig is the startup configuration for a TaskManager.
type TaskManagerConfig struct {
	ID                string
	SlotCount         int
	Messager          messager.IMessager
	State             TaskStateStore
	ApprovalInfo      executor.HumanApprovalStore
	APIServerURL      string // API server URL for runtime resource resolution
	Tenant            string // default tenant for API calls
	Logger            *utils.Logger
	HeartbeatInterval time.Duration
	SandboxMessager   messager.IMessager
	SandboxPolicy     *model.SandboxPolicy
	SandboxWorkspace  string
	SandboxDeploymentEnabled bool
	HttpClient        model.IFlowgentAPIClient // unified HTTP client (x402-aware when payments enabled)
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

	// Runtime resolvers — TM owns MCP/agent/LLM lifecycle, resolved via API at execution time.
	apiClient := client.NewFlowgentClient(cfg.APIServerURL)
	mcpMgr := mcp.NewMcpManager(cfg.HttpClient)
	llmLoader := &client.LlmProviderClient{Client: apiClient, Tenant: cfg.Tenant}
	llmClient := llm.NewLlmProviderManager(llmLoader)

	// Load MCP server definitions from DB (via apiserver API).
	mcpLoader := &client.McpProviderClient{Client: apiClient, Tenant: cfg.Tenant}
	if mcps, err := mcpLoader.ListMCPs(context.Background()); err == nil {
		slog.Debug("taskmanager loaded MCP servers", "count", len(mcps))
		for _, m := range mcps {
			if !m.Enabled || m.Name == "" {
				slog.Debug("taskmanager skip MCP", "name", m.Name, "enabled", m.Enabled)
				continue
			}
			slog.Debug("taskmanager register MCP", "name", m.Name, "type", m.Type, "url", m.URL)
			mcpMgr.Register(m.Name, m.URL, m.Headers)
		}
	} else {
		slog.Warn("taskmanager ListMCPs failed", "err", err)
	}
	// Also load LLM providers
	if providers, err := llmLoader.ListProviders(context.Background()); err == nil {
		slog.Debug("taskmanager loaded LLM providers", "count", len(providers))
	} else {
		slog.Warn("taskmanager ListProviders failed", "err", err)
	}

	router := executor.NewTaskExecutorRouter()
	agentExec := executor.NewAgentExecutor(llmClient, apiClient, cfg.Tenant)
	agentExec.SetKnowledgeRetriever(apiClient) // RAG: cross-workflow knowledge injection
	router.Register(agentExec)
	router.Register(&executor.ConditionExecutor{})
	router.Register(executor.NewToolExecutor(mcpMgr, cfg.HttpClient))
	router.Register(executor.NewSupervisorExecutor(llmClient, apiClient, cfg.Tenant))
	router.Register(&executor.CommitteeExecutor{})
	router.Register(&executor.MapExecutor{})
	router.Register(&executor.JoinExecutor{})
	router.Register(&executor.SubflowExecutor{})
	router.Register(executor.NewHumanExecutor(cfg.ApprovalInfo))
	router.Register(&executor.NoopExecutor{})
	router.Register(&executor.SkillExecutor{})
	router.Register(executor.NewSandboxExecutor(cfg.SandboxMessager, cfg.SandboxPolicy, cfg.SandboxWorkspace))

	metrics := NewTaskManagerMetrics()

	tm := &TaskManager{
		ID:      cfg.ID,
		router:  router,
		queue:   cfg.Messager,
		state:   cfg.State,
		metrics: metrics,
		logger:  cfg.Logger,
		stopCh:  make(chan struct{}),
	}

	for i := 0; i < cfg.SlotCount; i++ {
		slotID := fmt.Sprintf("%s-slot-%d", cfg.ID, i)
		sw := NewSlotWorker(slotID, cfg.ID, cfg.Messager, router, cfg.State, metrics)
		tm.slotWorkers = append(tm.slotWorkers, sw)
	}

	if !cfg.SandboxDeploymentEnabled && cfg.SandboxMessager != nil {
		embeddedRunner := sandbox.NewFlowgentSandboxManager(
			cfg.ID+"-sb", cfg.SandboxMessager, "", cfg.SandboxWorkspace, cfg.SandboxPolicy)
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
func (tm *TaskManager) ExecutePlan(ctx context.Context, plan *entities.ExecutionPlan, task *entities.TaskRunInfo) (*entities.TaskResult, error) {
	task.Input = plan.Input
	scope := map[string]map[string]any{"input": plan.Input}
	result, err := tm.router.Execute(ctx, plan, scope)
	if err != nil {
		return nil, err
	}
	task.Output = result.Output
	task.Status = entities.Success
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

func taskTypeAttr(t entities.TaskType) attribute.KeyValue {
	return attribute.String("task_type", string(t))
}
