package engine

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
	"github.com/flowgent-labs/flowgent/src/util"
)

// Store is the persistence layer interface, aliased from the store package.
// Engine tests use MockStore (in testing.go) which also implements this type.
type Store = interface {
	model.HumanApprovalStore
	CreateTaskRun(ctx context.Context, task *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error)
	GetTaskRunsByAgentFlowRun(ctx context.Context, agentFlowRunID string) ([]model.TaskRun, error)
	LogSupervisorDecision(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error
	GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error)

	SaveAgentFlowDefinition(ctx context.Context, d *model.AgentFlowVersion) error
	ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error)
	GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error)

	SaveExecutionPlan(ctx context.Context, plan *model.ExecutionPlan) error
	LoadExecutionPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error)
	ListExecutionPlans(ctx context.Context, agentFlowRunID string) ([]*model.ExecutionPlan, error)
	SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error
	LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error)
	ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error
	ReleaseLease(ctx context.Context, planID string) error

	DB() *sql.DB
}

type MCPClient interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
}

type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, model string, temperature float64) (string, error)
}

// TaskManager is a persistent worker that consumes ExecutionPlans from
// a queue (MQTT or local) and executes them via a pool of SlotWorkers.
// Designed as a long-lived K8s Deployment pod.
type TaskManager struct {
	ID          string
	slotWorkers []*SlotWorker
	router      *TaskExecutorRouter
	heartbeat   *HeartbeatPump
	queue       queue.Queue
	store       Store
	metrics     *TaskManagerMetrics
	logger      *util.Logger
	mu          sync.Mutex
	stopCh      chan struct{}
	stopped     bool
}

// TaskManagerConfig configures a TaskManager.
type TaskManagerConfig struct {
	ID              string
	SlotCount       int
	Queue           queue.Queue
	Store           Store
	Agents          []*config.AgentDef
	MCPClients      map[string]MCPClient
	LLMClient       LLMClient
	Logger          *util.Logger
	HeartbeatInterval time.Duration
}

func NewTaskManager(cfg *TaskManagerConfig) (*TaskManager, error) {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("tm-%d", time.Now().UnixNano())
	}
	if cfg.SlotCount <= 0 {
		cfg.SlotCount = 4
	}

	router := NewTaskExecutorRouter()
	router.Register(NewAgentExecutor(cfg.LLMClient, cfg.Agents))
	router.Register(&ConditionExecutor{})
	router.Register(NewToolExecutor(cfg.MCPClients))
	router.Register(NewSupervisorExecutor(cfg.LLMClient, cfg.Agents, cfg.Store))
	router.Register(&TribunalExecutor{})
	router.Register(&MapExecutor{})
	router.Register(&JoinExecutor{})
	router.Register(&SubflowExecutor{})
	router.Register(NewHumanExecutor(cfg.Store))
	router.Register(&NoopExecutor{})

	metrics := NewTaskManagerMetrics()

	hb := NewHeartbeatPump(cfg.ID, cfg.Queue, cfg.HeartbeatInterval)

	tm := &TaskManager{
		ID:        cfg.ID,
		router:    router,
		heartbeat: hb,
		queue:     cfg.Queue,
		store:     cfg.Store,
		metrics:   metrics,
		logger:    cfg.Logger,
		stopCh:    make(chan struct{}),
	}

	// Create slot workers
	for i := 0; i < cfg.SlotCount; i++ {
		slotID := fmt.Sprintf("%s-slot-%d", cfg.ID, i)
		sw := NewSlotWorker(slotID, cfg.ID, cfg.Queue, router, cfg.Store, metrics)
		tm.slotWorkers = append(tm.slotWorkers, sw)
	}

	return tm, nil
}

// Start begins the heartbeat pump and all slot workers.
func (tm *TaskManager) Start(ctx context.Context) error {
	tm.heartbeat.Start(ctx)
	for _, sw := range tm.slotWorkers {
		go sw.Loop(ctx)
	}
	tm.logger.Info("task manager started", "id", tm.ID, "slots", len(tm.slotWorkers))
	return nil
}

// Stop signals all workers to gracefully finish in-flight tasks and stop.
func (tm *TaskManager) Stop() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if !tm.stopped {
		tm.stopped = true
		close(tm.stopCh)
	}
}
