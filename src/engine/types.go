package engine

import (
	"context"
	"database/sql"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
)

// Provider identifies the resource management backend.
type Provider string

const (
	ProviderLocal      Provider = "local"
	ProviderKubernetes Provider = "kubernetes"
)

// MCPClient abstracts an MCP server connection.
type MCPClient interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
}

// LLMClient abstracts an LLM provider.
type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, model string, temperature float64) (string, error)
}

// Store is the persistence layer interface used by all engine components.
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
