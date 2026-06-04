package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

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
