package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

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
