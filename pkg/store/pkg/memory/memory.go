package memory

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IMemoryStore is the node memory entity store interface.
type IMemoryStore interface {
	Get(ctx context.Context, id string) (*entities.MemoryInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.MemoryInfo], error)
	Save(ctx context.Context, entity *entities.MemoryInfo) error
	Delete(ctx context.Context, id string) error
	UpsertMemory(ctx context.Context, mem *entities.MemoryInfo) error
	SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]entities.MemoryInfo, error)
	ListByFlow(ctx context.Context, flowID string) ([]entities.MemoryInfo, error)
}
