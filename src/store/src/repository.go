package store

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// ─── Base ────────────────────────────────────────────────

// IBaseStore provides DB access.
type IBaseStore interface {
	DB() any
}

// ─── Entity Repositories ─────────────────────────────────

// IAgentFlowStore manages AgentFlow definitions (spec, version, CRUD).
type IAgentFlowStore interface {
	SaveAgentFlow(ctx context.Context, def *model.AgentFlowVersion) error
	GetAgentFlow(ctx context.Context, id string) (*model.AgentFlowVersion, error)
	GetAgentFlowVersion(ctx context.Context, id string, version int64) (*model.AgentFlowVersion, error)
	ListAgentFlows(ctx context.Context) ([]model.AgentFlowVersion, error)
	DeleteAgentFlow(ctx context.Context, id string) error
	SaveAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error
	GetAgentFlowSpec(ctx context.Context, id string) (*model.AgentFlowSpec, error)
}

// IFlowRunStore manages AgentFlowRun instances.
type IFlowRunStore interface {
	CreateFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	UpdateFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	GetFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error)
	DeleteFlowRun(ctx context.Context, id string) error
	CancelFlowRun(ctx context.Context, id string) error
}

// ITaskPlanStore manages TaskRuns, ExecutionPlans, Checkpoints, Leases, and Supervisor logs.
type ITaskPlanStore interface {
	CreateTaskRun(ctx context.Context, task *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	ListTaskRunsByFlow(ctx context.Context, flowRunID string) ([]model.TaskRun, error)
	GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error)

	SavePlan(ctx context.Context, plan *model.ExecutionPlan) error
	LoadPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error)
	ListPlans(ctx context.Context, flowRunID string) ([]*model.ExecutionPlan, error)

	SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error
	LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error)

	ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error
	ReleaseLease(ctx context.Context, planID string) error

	LogSupervisor(ctx context.Context, flowRunID, taskRunID string, input, decision map[string]any) error
}

// IAgentStore manages AgentDef entities.
type IAgentStore interface {
	SaveAgent(ctx context.Context, agent *model.AgentDef) error
	GetAgent(ctx context.Context, name string) (*model.AgentDef, error)
	ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error)
	DeleteAgent(ctx context.Context, name string) error
}

// IApprovalStore manages HumanApproval entities.
type IApprovalStore interface {
	CreateApproval(ctx context.Context, a *model.HumanApproval) error
	GetApproval(ctx context.Context, token string) (*model.HumanApproval, error)
	UpdateApproval(ctx context.Context, a *model.HumanApproval) error
	ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error)
}

// INotifierStore manages NotifierChannel and SubscriptionRoute entities.
type INotifierStore interface {
	SaveChannel(ctx context.Context, ch *model.NotifierChannel) error
	GetChannel(ctx context.Context, id string) (*model.NotifierChannel, error)
	ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error)
	DeleteChannel(ctx context.Context, id string) error

	SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error
	GetRoutesByFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error)
	DeleteRoute(ctx context.Context, id string) error
	DeleteRoutesByPod(ctx context.Context, podID string) error
	CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error)
}

// ILlmProviderStore manages LlmProvider entities.
type ILlmProviderStore interface {
	SaveProvider(ctx context.Context, p *model.LlmProvider) error
	GetProvider(ctx context.Context, id string) (*model.LlmProvider, error)
	ListProviders(ctx context.Context, tenantID string) ([]model.LlmProvider, error)
	DeleteProvider(ctx context.Context, id string) error
}

// ─── Composed Store ──────────────────────────────────────

// IStore composes all entity repository interfaces (JPA SessionFactory pattern).
type IStore interface {
	IBaseStore
	IAgentFlowStore
	IFlowRunStore
	ITaskPlanStore
	IAgentStore
	IApprovalStore
	INotifierStore
	ILlmProviderStore
}
