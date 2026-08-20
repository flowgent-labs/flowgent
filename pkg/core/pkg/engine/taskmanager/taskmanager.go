package taskmanager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/secretref"
	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/core/pkg/llm"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
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
	ID                       string
	SlotCount                int
	Messager                 messager.IMessager
	State                    TaskStateStore
	ApprovalInfo             executor.HumanApprovalStore
	APIServerURL             string // API server URL for runtime resource resolution
	Namespace                string // default namespace for API calls
	RuntimeClusterID         string // runtime cluster scope for work and readiness
	Logger                   *utils.Logger
	HeartbeatInterval        time.Duration
	SandboxMessager          messager.IMessager
	SandboxPolicy            *model.SandboxPolicy
	SandboxWorkspace         string
	SandboxDeploymentEnabled bool
	HttpClient               model.IFlowgentAPIClient // unified HTTP client (x402-aware when payments enabled)
}

// TaskManager is a persistent worker that consumes ExecutionPlans from
// a queue (MQTT or local) and executes them via a pool of SlotWorkers.
type TaskManager struct {
	ID                string
	slotWorkers       []*SlotWorker
	router            *executor.TaskExecutorRouter
	queue             messager.IMessager
	state             TaskStateStore
	metrics           *TaskManagerMetrics
	logger            *utils.Logger
	namespace         string
	runtimeClusterID  string
	heartbeatInterval time.Duration
	mu                sync.Mutex
	stopCh            chan struct{}
	stopped           bool
}

func NewTaskManager(cfg *TaskManagerConfig) (*TaskManager, error) {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("tm-%d", time.Now().UnixNano())
	}
	if cfg.SlotCount <= 0 {
		cfg.SlotCount = 4
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.RuntimeClusterID == "" {
		return nil, fmt.Errorf("taskmanager requires runtime_cluster_id")
	}

	// Runtime resolvers — TM owns MCP/agent/LLM lifecycle, resolved via API at execution time.
	apiClient := client.NewFlowgentClient(cfg.APIServerURL)
	mcpMgr := mcp.NewMcpManager(cfg.HttpClient)
	llmLoader := &client.LlmProviderClient{Client: apiClient, Namespace: cfg.Namespace}
	llmClient := llm.NewLlmProviderManager(llmLoader)

	if cfg.APIServerURL != "" {
		// Load MCP server definitions from DB (via apiserver API).
		mcpLoader := &client.McpProviderClient{Client: apiClient, Namespace: cfg.Namespace}
		if mcps, err := mcpLoader.ListMCPs(context.Background()); err == nil {
			slog.Info("taskmanager loaded MCP servers", "count", len(mcps))
			for _, m := range mcps {
				slog.Info("taskmanager MCP candidate", "name", m.Name, "enabled", m.Enabled, "type", m.Type)
				if !m.Enabled || m.Name == "" {
					slog.Info("taskmanager skip MCP", "name", m.Name, "enabled", m.Enabled)
					continue
				}
				slog.Info("taskmanager register MCP", "name", m.Name, "type", m.Type, "url", m.URL)
				// Resolve persisted credential references from the K8s Secret envFrom.
				persistedHeaders := m.Headers
				if m.HeaderRefs != nil {
					persistedHeaders = m.HeaderRefs
				}
				headers := make(map[string]string)
				for k, v := range persistedHeaders {
					resolved, resolveErr := secretref.Expand(v)
					if resolveErr != nil {
						return nil, fmt.Errorf("resolve MCP %s header %s: %w", m.Name, k, resolveErr)
					}
					headers[k] = resolved
				}
				url := os.ExpandEnv(m.URL)
				slog.Info("taskmanager MCP resolved", "name", m.Name, "url", url)
				mcpMgr.Register(m.Name, url, headers)
			}
		} else {
			slog.Warn("taskmanager ListMCPs failed", "err", err)
		}
		if providers, err := llmLoader.ListProviders(context.Background()); err == nil {
			slog.Debug("taskmanager loaded LLM providers", "count", len(providers))
		} else {
			slog.Warn("taskmanager ListProviders failed", "err", err)
		}
	}

	router := executor.NewTaskExecutorRouter()
	agentExec := executor.NewAgentExecutor(llmClient, apiClient, cfg.Namespace)
	agentExec.SetKnowledgeRetriever(apiClient) // RAG: cross-workflow knowledge injection
	router.Register(agentExec)
	router.Register(&executor.ConditionExecutor{})
	router.Register(executor.NewToolExecutor(mcpMgr, cfg.HttpClient))
	router.Register(executor.NewSupervisorExecutor(llmClient, apiClient, cfg.Namespace))
	router.Register(&executor.CommitteeExecutor{})
	router.Register(&executor.MapExecutor{})
	router.Register(&executor.JoinExecutor{})
	router.Register(&executor.SubflowExecutor{})
	router.Register(executor.NewHumanExecutor(cfg.ApprovalInfo))
	router.Register(&executor.NoopExecutor{})
	router.Register(&executor.SkillExecutor{})
	sandboxExec := executor.NewSandboxExecutor(cfg.SandboxMessager, cfg.SandboxPolicy, cfg.SandboxWorkspace)
	sandboxExec.SetRuntimeConfigResolver(apiClient)
	router.Register(sandboxExec)

	metrics := NewTaskManagerMetrics()

	tm := &TaskManager{
		ID:                cfg.ID,
		router:            router,
		queue:             cfg.Messager,
		state:             cfg.State,
		metrics:           metrics,
		logger:            cfg.Logger,
		namespace:         cfg.Namespace,
		runtimeClusterID:  cfg.RuntimeClusterID,
		heartbeatInterval: cfg.HeartbeatInterval,
		stopCh:            make(chan struct{}),
	}

	for i := 0; i < cfg.SlotCount; i++ {
		slotID := fmt.Sprintf("%s-slot-%d", cfg.ID, i)
		sw := NewSlotWorker(slotID, cfg.ID, cfg.Namespace, cfg.RuntimeClusterID, cfg.Messager, router, cfg.State, metrics)
		tm.slotWorkers = append(tm.slotWorkers, sw)
	}

	if !cfg.SandboxDeploymentEnabled && cfg.SandboxMessager != nil {
		embeddedRunner := sandbox.NewFlowgentSandboxManager(
			cfg.ID+"-sb", cfg.SandboxMessager, "", cfg.SandboxWorkspace, cfg.SandboxPolicy)
		embeddedRunner.SetScope(cfg.Namespace, cfg.RuntimeClusterID)
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
	for _, sw := range tm.slotWorkers {
		if err := sw.Subscribe(ctx); err != nil {
			return fmt.Errorf("subscribe slot worker %s: %w", sw.id, err)
		}
	}
	// Readiness is emitted only after every slot handler is registered. The
	// JobManager waits for this scoped lease before publishing the first plan.
	startHeartbeat(ctx, tm.ID, tm.namespace, tm.runtimeClusterID, tm.queue, tm.heartbeatInterval)
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
	if task == nil {
		task = &entities.TaskRunInfo{}
	}
	if task.ID == "" {
		task.BaseEntity.ID = plan.TaskID
	}
	task.AgentFlowRunID = plan.AgentFlowRunID
	task.NodeID = plan.NodeID
	task.Input = plan.Input
	task.RetryCount = plan.RetryCount
	task.MaxRetries = plan.MaxRetries
	task.ExecID = fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1)
	task.Sequence = plan.RetryCount + 1
	startedAt := time.Now().UTC()
	if plan.StartedAt != nil {
		startedAt = *plan.StartedAt
	} else {
		plan.StartedAt = &startedAt
	}
	task.StartedAt = &startedAt
	scope := map[string]map[string]any{"input": plan.Input}
	result, err := tm.router.Execute(ctx, plan, scope)
	if err != nil {
		task.Status = entities.Failed
		task.Error = err.Error()
		now := time.Now().UTC()
		task.FinishedAt = &now
		plan.FinishedAt = &now
		if tm.state != nil {
			_ = tm.state.SaveTask(ctx, task)
		}
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("executor returned no result")
	}
	task.Output = result.Output
	task.Error = result.Error
	if result.Error != "" {
		task.Status = entities.Failed
	} else {
		task.Status = entities.Success
	}
	now := time.Now().UTC()
	task.FinishedAt = &now
	plan.FinishedAt = &now
	if tm.state != nil {
		_ = tm.state.SaveTask(ctx, task)
	}
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
