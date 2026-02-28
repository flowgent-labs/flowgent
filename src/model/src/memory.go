// Package model defines the shared domain types for the Flowgent engine.
//
// File: memory.go — Agent node memory, scoped by (flow_id, node_id).
// Persists across runs: if a flow is interrupted and restarted, each node's
// memory from previous executions is available for LLM context enrichment.
package model

import "time"

// NodeMemory stores accumulated execution context for a (flow, node) pair.
// Scoped by FlowID + NodeID; persists across ALL runs of the same flow definition.
// When NodeID is empty, this is flow-level shared memory accessible to every node.
type NodeMemory struct {
	FlowID    string         `json:"flow_id"`   // agentflow definition ID
	NodeID    string         `json:"node_id"`   // DAG node ID (empty = flow-level shared)
	Content   string         `json:"content"`   // accumulated execution context
	Embedding []float32      `json:"embedding"` // vector for similarity search
	Metadata  map[string]any `json:"metadata"`  // {retry_count, last_error, last_model, token_usage, ...}
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
