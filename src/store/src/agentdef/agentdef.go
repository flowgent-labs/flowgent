package agentdef

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IAgentDefStore is the agent definition entity store interface.
type IAgentDefStore interface {
	Get(ctx context.Context, name string) (*model.AgentDef, error)
	Select(ctx context.Context, page, size int) (*model.Page[model.AgentDef], error)
	Save(ctx context.Context, entity *model.AgentDef) error
	Delete(ctx context.Context, name string) error
}
