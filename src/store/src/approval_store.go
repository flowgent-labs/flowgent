package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IApprovalStore manages HumanApproval entities.
type IApprovalStore interface {
	CreateApproval(ctx context.Context, a *model.HumanApproval) error
	GetApproval(ctx context.Context, token string) (*model.HumanApproval, error)
	UpdateApproval(ctx context.Context, a *model.HumanApproval) error
	ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error)
}
