package entities

import "time"

// MemoryInfo stores accumulated execution context for a (flow, node) pair.
type MemoryInfo struct {
	FlowID    string         `json:"flow_id"`
	NodeID    string         `json:"node_id"`
	Content   string         `json:"content"`
	Embedding []float32      `json:"embedding"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
