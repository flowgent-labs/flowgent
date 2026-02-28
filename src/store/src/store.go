package store

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// Store is the unified persistence interface for the flowgent engine.
type Store interface {
	// AgentFlow definitions
	SaveAgentFlowDefinition(ctx context.Context, def *model.AgentFlowVersion) error
	GetLatestAgentFlowDefinition(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error)
	GetAgentFlowDefinition(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error)
	ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error)

	// AgentFlow definitions — dynamic CRUD (DB-backed standard path)
	DeleteAgentFlowDefinition(ctx context.Context, agentFlowID string) error
	UpdateAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error
	GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error)

	// Agent definitions (DB-backed)
	SaveAgent(ctx context.Context, agent *model.AgentDef) error
	GetAgent(ctx context.Context, name string) (*model.AgentDef, error)
	ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error)
	DeleteAgent(ctx context.Context, name string) error

	// AgentFlow runs
	CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error)
	DeleteAgentFlowRun(ctx context.Context, id string) error
	CancelAgentFlowRun(ctx context.Context, id string) error

	// Task runs
	CreateTaskRun(ctx context.Context, task *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	GetTaskRunsByAgentFlowRun(ctx context.Context, agentFlowRunID string) ([]model.TaskRun, error)
	GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error)

	// Human approvals
	CreateHumanApproval(ctx context.Context, approval *model.HumanApproval) error
	GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error)
	UpdateHumanApproval(ctx context.Context, approval *model.HumanApproval) error
	GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error)

	// Supervisor log
	LogSupervisorDecision(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error

	// Notification channels
	SaveNotifierChannel(ctx context.Context, ch *model.NotifierChannel) error
	GetNotifierChannel(ctx context.Context, id string) (*model.NotifierChannel, error)
	ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error)
	DeleteNotifierChannel(ctx context.Context, id string) error

	// Subscription routes (clustered WebSocket delivery)
	SaveSubscriptionRoute(ctx context.Context, route *model.SubscriptionRoute) error
	GetSubscriptionRoutesByAgentFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error)
	DeleteSubscriptionRoute(ctx context.Context, id string) error
	DeleteSubscriptionRoutesByPod(ctx context.Context, podID string) error
	CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error)

	// ExecutionPlan methods
	SaveExecutionPlan(ctx context.Context, plan *model.ExecutionPlan) error
	LoadExecutionPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error)
	ListExecutionPlans(ctx context.Context, agentFlowRunID string) ([]*model.ExecutionPlan, error)

	// Checkpoint methods
	SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error
	LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error)

	// Lease methods
	ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error
	ReleaseLease(ctx context.Context, planID string) error

	// DB access for memory store and RAG
	DB() any
}
