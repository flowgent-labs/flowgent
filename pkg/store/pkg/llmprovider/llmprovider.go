package llmprovider

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ILlmProviderStore is the LLM provider entity store interface.
type ILlmProviderStore interface {
	Get(ctx context.Context, id string) (*entities.LlmProviderInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.LlmProviderInfo], error)
	Save(ctx context.Context, entity *entities.LlmProviderInfo) error
	Delete(ctx context.Context, id string) error
}
