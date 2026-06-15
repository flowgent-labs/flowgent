package agentflow

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IAgentFlowStore is the agentflow entity store interface.
type IAgentFlowStore interface {
	Get(ctx context.Context, id string) (*entities.AgentFlowVersionInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.AgentFlowVersionInfo], error)
	Save(ctx context.Context, entity *entities.AgentFlowVersionInfo) error
	Delete(ctx context.Context, id string) error
	GetVersion(ctx context.Context, id string, version int64) (*entities.AgentFlowVersionInfo, error)
	SaveSpec(ctx context.Context, spec *entities.AgentFlowInfo, createdBy, comment string) error
	GetSpec(ctx context.Context, id string) (*entities.AgentFlowInfo, error)
}
