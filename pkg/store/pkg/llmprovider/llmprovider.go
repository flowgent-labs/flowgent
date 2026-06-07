package llmprovider

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ILlmProviderStore is the LLM provider entity store interface.
type ILlmProviderStore interface {
	Get(ctx context.Context, id string) (*model.LlmProvider, error)
	Select(ctx context.Context, req model.PageRequest) (*model.Page[model.LlmProvider], error)
	Save(ctx context.Context, entity *model.LlmProvider) error
	Delete(ctx context.Context, id string) error
}
