package agentdef

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IAgentStore is the agent definition entity store interface.
type IAgentStore interface {
	Get(ctx context.Context, name string) (*model.AgentDef, error)
	Select(ctx context.Context, offset, limit int) ([]*model.AgentDef, error)
	Save(ctx context.Context, entity *model.AgentDef) error
	Delete(ctx context.Context, name string) error
}
