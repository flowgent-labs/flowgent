package store

import (
	"context"

	"github.com/flowgent-labs/flowgent/src/model"
)

// MemoryStore is the persistence interface for agent memories and knowledge base.
type MemoryStore interface {
	// Memories (episodic, procedural, semantic)
	SaveMemory(ctx context.Context, m *model.Memory) error
	GetMemory(ctx context.Context, id string) (*model.Memory, error)
	ListMemories(ctx context.Context, agentID string, memType model.MemoryType, limit int) ([]model.Memory, error)
	DeleteMemory(ctx context.Context, id string) error
	UpdateMemory(ctx context.Context, m *model.Memory) error
	SearchMemories(ctx context.Context, agentID string, embedding []float32, topK int) ([]model.Memory, error)

	// Knowledge base
	SaveKnowledge(ctx context.Context, k *model.KnowledgeEntry) error
	GetKnowledge(ctx context.Context, id string) (*model.KnowledgeEntry, error)
	SearchKnowledge(ctx context.Context, embedding []float32, category string, topK int) ([]model.KnowledgeEntry, error)
	ListKnowledge(ctx context.Context, category string, limit int) ([]model.KnowledgeEntry, error)
	DeleteKnowledge(ctx context.Context, id string) error

	Close() error
}
