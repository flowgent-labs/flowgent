package model

import "time"

type MemoryType string

const (
	MemoryEpisodic   MemoryType = "episodic"
	MemoryProcedural MemoryType = "procedural"
	MemorySemantic   MemoryType = "semantic"
)

type Memory struct {
	ID             string         `json:"id" yaml:"id"`
	AgentID        string         `json:"agent_id" yaml:"agent_id"`
	AgentFlowRunID string         `json:"agentflow_run_id" yaml:"agentflow_run_id"`
	Type           MemoryType     `json:"type" yaml:"type"`
	Content        string         `json:"content" yaml:"content"`
	Embedding      []float32      `json:"embedding" yaml:"embedding"`
	Tags           []string       `json:"tags" yaml:"tags"`
	Metadata       map[string]any `json:"metadata" yaml:"metadata"`
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
