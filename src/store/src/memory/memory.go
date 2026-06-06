package memory

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IMemoryStore is the node memory entity store interface.
type IMemoryStore interface {
	Get(ctx context.Context, id string) (*model.NodeMemory, error)
	Select(ctx context.Context, page, pageSize int) (*model.Page[model.NodeMemory], error)
	Save(ctx context.Context, entity *model.NodeMemory) error
	Delete(ctx context.Context, id string) error
	UpsertMemory(ctx context.Context, mem *model.NodeMemory) error
	SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]model.NodeMemory, error)
	ListByFlow(ctx context.Context, flowID string) ([]model.NodeMemory, error)
}
