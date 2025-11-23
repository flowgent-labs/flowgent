package store

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/src/model"
)

// Store is the unified persistence interface for the flowgent engine.
type Store interface {
	// AgentFlow definitions
	SaveAgentFlowDefinition(ctx context.Context, def *model.AgentFlowVersion) error
	GetLatestAgentFlowDefinition(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error)
	GetAgentFlowDefinition(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error)
	ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error)

	// AgentFlow runs
	CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error)

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

	// Idempotency
	CheckIdempotency(ctx context.Context, key string) (bool, error)
	AcquireIdempotency(ctx context.Context, key, taskRunID, execID string) error

	// Supervisor log
	LogSupervisorDecision(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error

	// DB access for memory store
	DB() *sql.DB
}
