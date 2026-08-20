package agent

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IAgentInfoStore is the agent info entity store interface.
type IAgentInfoStore interface {
	Get(ctx context.Context, namespace, name string) (*entities.AgentInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error)
	Save(ctx context.Context, entity *entities.AgentInfo) error
	Delete(ctx context.Context, namespace, name string) error
}
