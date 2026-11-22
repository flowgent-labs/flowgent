package taskmanager

import (
	"context"
	"encoding/json"
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

// NodeRunStateStore is the narrow state interface TM workers need for persistence.
// Implementations call the apiserver REST API (never direct DB).
type NodeRunStateStore interface {
	SaveNodeRun(ctx context.Context, noderun *entities.NodeRun) error
}

// TaskManagerConfig is the startup configuration for a TaskManager.
type TaskManagerConfig struct {
	ID                       string
	SlotCount                int
	Messager                 messager.IMessager
	State                    NodeRunStateStore
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
	state             NodeRunStateStore
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
				m.Normalize()
				slog.Info("taskmanager MCP candidate", "name", m.Name, "enabled", m.Enabled, "transport", m.Transport)
				if !m.Enabled || m.Name == "" {
					slog.Info("taskmanager skip MCP", "name", m.Name, "enabled", m.Enabled)
					continue
				}
				slog.Info("taskmanager register MCP", "name", m.Name, "transport", m.Transport, "rpc_url", m.RPCURL)
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
				url := os.ExpandEnv(m.RPCURL)
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
	router.Register(&executor.SubGraphExecutor{})
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
		embeddedRunner.SetCheckpointWriter(apiClient)
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
	tm.logger.Info("noderun manager started", "id", tm.ID, "slots", len(tm.slotWorkers))
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
func (tm *TaskManager) ExecutePlan(ctx context.Context, plan *entities.ExecutionPlan, noderun *entities.NodeRun) (*entities.NodeResult, error) {
	if noderun == nil {
		noderun = &entities.NodeRun{}
	}
	if noderun.ID == "" {
		noderun.BaseEntity.ID = plan.NodeRunID
	}
	noderun.GraphRunID = plan.GraphRunID
	noderun.NodeInstanceID = plan.NodeInstanceID
	noderun.GraphRevision = plan.GraphRevision
	noderun.SubGraphInstanceID = plan.SubGraphInstanceID
	noderun.GraphNodeID = plan.GraphNodeID
	noderun.NodeInstanceID = plan.NodeInstanceID
	noderun.DispatchEpoch = plan.DispatchEpoch
	noderun.FencingToken = plan.FencingToken
	noderun.Metadata = map[string]any{"node": plan.Node}
	noderun.Input = plan.Input
	noderun.Attempt = plan.RetryCount + 1
	noderun.MaxRetries = plan.MaxRetries
	noderun.ExecutionID = fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1)
	noderun.Sequence = plan.RetryCount + 1
	noderun.Normalize()
	startedAt := time.Now().UTC()
	if plan.StartedAt != nil {
		startedAt = *plan.StartedAt
	} else {
		plan.StartedAt = &startedAt
	}
	noderun.StartedAt = &startedAt
	noderun.Status = entities.NodeRunRunning
	plan.State = entities.NodeRunRunning
	if tm.state != nil {
		if err := tm.state.SaveNodeRun(ctx, noderun); err != nil {
			return nil, err
		}
	}
	scope := map[string]map[string]any{"input": plan.Input}
	result, err := tm.router.Execute(ctx, plan, scope)
	if err != nil {
		noderun.Status = entities.NodeRunFailed
		noderun.Error = err.Error()
		now := time.Now().UTC()
		noderun.FinishedAt = &now
		plan.FinishedAt = &now
		if tm.state != nil {
			if saveErr := tm.state.SaveNodeRun(ctx, noderun); saveErr != nil {
				return nil, fmt.Errorf("persist noderun result: %w", saveErr)
			}
		}
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("executor returned no result")
	}
	noderun.WorkspaceSnapshot = result.WorkspaceSnapshot
	if result.WorkspaceSnapshot != nil {
		noderun.WorkspaceVersion = result.WorkspaceSnapshot.ID
	}
	plan.Result = result
	if result.Checkpoint != nil {
		plan.Checkpoint = result.Checkpoint
		data, _ := json.Marshal(result.Checkpoint)
		_ = json.Unmarshal(data, &noderun.Checkpoint)
	}
	noderun.Output = result.Output
	noderun.Error = result.Error
	if result.State == entities.NodeRunWaiting {
		noderun.Status = entities.NodeRunWaiting
		noderun.FinishedAt = nil
		plan.FinishedAt = nil
		if tm.state != nil {
			if err := tm.state.SaveNodeRun(ctx, noderun); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
	if result.Error != "" {
		noderun.Status = entities.NodeRunFailed
	} else {
		noderun.Status = entities.NodeRunSucceeded
	}
	now := time.Now().UTC()
	noderun.FinishedAt = &now
	plan.FinishedAt = &now
	if tm.state != nil {
		if saveErr := tm.state.SaveNodeRun(ctx, noderun); saveErr != nil {
			return nil, fmt.Errorf("persist noderun result: %w", saveErr)
		}
	}
	if noderun.Status == entities.NodeRunSucceeded {
		slog.Info("taskmanager noderun succeeded", "tm_id", tm.ID, "plan_id", plan.PlanID,
			"node_run_id", noderun.ID, "node_instance_id", plan.NodeInstanceID)
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

func taskTypeAttr(t entities.NodeKind) attribute.KeyValue {
	return attribute.String("node_kind", string(t))
}
