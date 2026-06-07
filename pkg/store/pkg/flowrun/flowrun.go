package flowrun

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// IFlowRunStore is the agentflow run entity store interface.
type IFlowRunStore interface {
	Get(ctx context.Context, id string) (*model.AgentFlowRun, error)
	Select(ctx context.Context, req model.PageRequest) (*model.Page[model.AgentFlowRun], error)
	Save(ctx context.Context, entity *model.AgentFlowRun) error
	Delete(ctx context.Context, id string) error
	Create(ctx context.Context, entity *model.AgentFlowRun) error
	Update(ctx context.Context, entity *model.AgentFlowRun) error
	Cancel(ctx context.Context, id string) error
}
