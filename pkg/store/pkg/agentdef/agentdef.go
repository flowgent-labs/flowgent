package agentdef

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IAgentDefStore is the agent definition entity store interface.
type IAgentDefStore interface {
	Get(ctx context.Context, name string) (*entities.AgentInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error)
	Save(ctx context.Context, entity *entities.AgentInfo) error
	Delete(ctx context.Context, name string) error
}
