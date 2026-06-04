package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IAgentStore manages AgentDef entities.
type IAgentStore interface {
	SaveAgent(ctx context.Context, agent *model.AgentDef) error
	GetAgent(ctx context.Context, name string) (*model.AgentDef, error)
	ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error)
	DeleteAgent(ctx context.Context, name string) error
}
