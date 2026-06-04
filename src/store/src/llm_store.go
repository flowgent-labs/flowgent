package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// ILlmProviderStore manages LlmProvider entities.
type ILlmProviderStore interface {
	SaveProvider(ctx context.Context, p *model.LlmProvider) error
	GetProvider(ctx context.Context, id string) (*model.LlmProvider, error)
	ListProviders(ctx context.Context, tenantID string) ([]model.LlmProvider, error)
	DeleteProvider(ctx context.Context, id string) error
}
