package approval

import (
	"context"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
)

// IApprovalStore is the human approval entity store interface.
type IApprovalStore interface {
	Get(ctx context.Context, token string) (*model.HumanApproval, error)
	Select(ctx context.Context, page, pageSize int) (*utils.Page[model.HumanApproval], error)
	Save(ctx context.Context, entity *model.HumanApproval) error
	Delete(ctx context.Context, token string) error
	CreateApproval(ctx context.Context, entity *model.HumanApproval) error
	UpdateApproval(ctx context.Context, entity *model.HumanApproval) error
	ListPending(ctx context.Context) ([]*model.HumanApproval, error)
}
