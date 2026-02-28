package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/src"
)

// NodeMemoryStore persists execution context scoped by (flow_id, node_id).
// Unlike run-scoped memory, NodeMemory persists across ALL runs of the same
// flow definition — restarts and retries automatically benefit from prior context.
//
// When nodeID is empty, the entry is flow-level shared memory accessible to every node.
type NodeMemoryStore interface {
	// GetMemory returns the memory for (flowID, nodeID), or nil if not found.
	GetMemory(ctx context.Context, flowID, nodeID string) (*model.NodeMemory, error)

	// UpsertMemory creates or updates the memory entry for (flowID, nodeID).
	UpsertMemory(ctx context.Context, mem *model.NodeMemory) error

	// SearchMemory returns the top-K memories within a flow most similar
	// to the given embedding. Searches across all nodes in the flow.
	SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]model.NodeMemory, error)

	// ListFlowMemories returns all memories for a flow definition, ordered by recency.
	ListFlowMemories(ctx context.Context, flowID string) ([]model.NodeMemory, error)

	// DeleteMemory removes the memory for (flowID, nodeID).
	DeleteMemory(ctx context.Context, flowID, nodeID string) error
}
