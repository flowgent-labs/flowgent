package flow

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IFlowInfoStore is the flow info entity store interface.
type IFlowInfoStore interface {
	Get(ctx context.Context, id string) (*entities.FlowVersionInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error)
	Save(ctx context.Context, entity *entities.FlowVersionInfo) error
	Delete(ctx context.Context, id string) error
	GetVersion(ctx context.Context, id string, version int64) (*entities.FlowVersionInfo, error)
	SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error
	GetSpec(ctx context.Context, id string) (*entities.FlowInfo, error)
}
