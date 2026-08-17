package flowrun

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IFlowRunStore is the agentflow run entity store interface.
type IFlowRunStore interface {
	Get(ctx context.Context, id string) (*entities.FlowRunInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowRunInfo], error)
	HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error)
	HasActiveForPool(ctx context.Context, namespace, poolID string) (bool, error)
	Save(ctx context.Context, entity *entities.FlowRunInfo) error
	Delete(ctx context.Context, id string) error
	Create(ctx context.Context, entity *entities.FlowRunInfo) error
	Update(ctx context.Context, entity *entities.FlowRunInfo) error
	Cancel(ctx context.Context, id string) error
}
