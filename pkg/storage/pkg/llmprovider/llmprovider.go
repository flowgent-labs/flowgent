package llmprovider

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ILlmProviderStore is the LLM provider entity store interface.
type ILlmProviderStore interface {
	Get(ctx context.Context, namespace, id string) (*entities.LlmProviderInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.LlmProviderInfo], error)
	Save(ctx context.Context, entity *entities.LlmProviderInfo) error
	Delete(ctx context.Context, namespace, id string) error
}
