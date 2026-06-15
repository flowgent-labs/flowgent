package approval

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IApprovalStore is the human approval entity store interface.
type IApprovalStore interface {
	Get(ctx context.Context, token string) (*entities.ApprovalInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.ApprovalInfo], error)
	Save(ctx context.Context, entity *entities.ApprovalInfo) error
	Delete(ctx context.Context, token string) error
	CreateApproval(ctx context.Context, entity *entities.ApprovalInfo) error
	UpdateApproval(ctx context.Context, entity *entities.ApprovalInfo) error
	ListPending(ctx context.Context) ([]*entities.ApprovalInfo, error)
}
