// Package model defines the shared domain types for the Flowgent engine.
//
// File: memory.go — Agent memory and knowledge store types.
//   Memory, KnowledgeEntry, MemoryType enum.
package model

import "time"

type MemoryType string

const (
	MemoryEpisodic   MemoryType = "episodic"
	MemoryProcedural MemoryType = "procedural"
	MemorySemantic   MemoryType = "semantic"
)

// Memory represents an episodic/procedural/semantic memory entry tied to an agent execution.
// Stored after each node execution and queried before LLM calls to enrich context.
type Memory struct {
	ID             string         `json:"id" yaml:"id"`
	AgentID        string         `json:"agent_id" yaml:"agent_id"`
	AgentFlowRunID string         `json:"agentflow_run_id" yaml:"agentflow_run_id"`
	NodeID         string         `json:"node_id" yaml:"node_id"`           // which DAG node created this memory
	Type           MemoryType     `json:"type" yaml:"type"`
	Content        string         `json:"content" yaml:"content"`           // LLM prompt/response, tool output, etc.
	Embedding      []float32      `json:"embedding" yaml:"embedding"`       // vector for similarity search
	RetryCount     int            `json:"retry_count" yaml:"retry_count"`   // which retry attempt
	Status         string         `json:"status" yaml:"status"`             // "success" | "failed" | "retrying"
	Tags           []string       `json:"tags" yaml:"tags"`
	Metadata       map[string]any `json:"metadata" yaml:"metadata"`         // extensible: token usage, model, latency, etc.
	TTL            *time.Time     `json:"ttl,omitempty" yaml:"ttl,omitempty"` // auto-expiry (nil = never)
	CreatedAt      time.Time      `json:"created_at" yaml:"created_at"`
}

type KnowledgeEntry struct {
	ID        string         `json:"id" yaml:"id"`
	Category  string         `json:"category" yaml:"category"`
	Title     string         `json:"title" yaml:"title"`
	Content   string         `json:"content" yaml:"content"`
	Embedding []float32      `json:"embedding" yaml:"embedding"`
	Tags      []string       `json:"tags" yaml:"tags"`
	Source    string         `json:"source" yaml:"source"`
	CreatedAt time.Time      `json:"created_at" yaml:"created_at"`
	UpdatedAt time.Time      `json:"updated_at" yaml:"updated_at"`
}
