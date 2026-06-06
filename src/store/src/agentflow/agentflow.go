package agentflow

import (
	"context"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
)

// IAgentFlowStore is the agentflow entity store interface.
type IAgentFlowStore interface {
	Get(ctx context.Context, id string) (*model.AgentFlowVersion, error)
	Select(ctx context.Context, page, pageSize int) (*utils.Page[model.AgentFlowVersion], error)
	Save(ctx context.Context, entity *model.AgentFlowVersion) error
	Delete(ctx context.Context, id string) error
	GetVersion(ctx context.Context, id string, version int64) (*model.AgentFlowVersion, error)
	SaveSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error
	GetSpec(ctx context.Context, id string) (*model.AgentFlowSpec, error)
}
