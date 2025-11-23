package store

import (
	"context"
	"fmt"

	"github.com/flowgent-labs/flowgent/src/model"
)

// NewMemory is a builder for model.Memory.
func NewMemory(agentID string) *memoryBuilder { return &memoryBuilder{m: &model.Memory{AgentID: agentID}} }

type memoryBuilder struct{ m *model.Memory }

func (b *memoryBuilder) Type(t model.MemoryType) *memoryBuilder { b.m.Type = t; return b }
func (b *memoryBuilder) AgentFlowRunID(id string) *memoryBuilder { b.m.AgentFlowRunID = id; return b }
func (b *memoryBuilder) Content(c string) *memoryBuilder       { b.m.Content = c; return b }
func (b *memoryBuilder) Embedding(e []float32) *memoryBuilder   { b.m.Embedding = e; return b }
func (b *memoryBuilder) Tags(t []string) *memoryBuilder          { b.m.Tags = t; return b }
func (b *memoryBuilder) Metadata(m map[string]any) *memoryBuilder { b.m.Metadata = m; return b }
func (b *memoryBuilder) Build() *model.Memory                    { return b.m }

// NewKnowledgeEntry is a builder for model.KnowledgeEntry.
func NewKnowledgeEntry(category, title, content string) *kbBuilder {
	return &kbBuilder{k: &model.KnowledgeEntry{Category: category, Title: title, Content: content}}
}

type kbBuilder struct{ k *model.KnowledgeEntry }

func (b *kbBuilder) Embedding(e []float32) *kbBuilder { b.k.Embedding = e; return b }
func (b *kbBuilder) Tags(t []string) *kbBuilder       { b.k.Tags = t; return b }
func (b *kbBuilder) Source(s string) *kbBuilder       { b.k.Source = s; return b }
func (b *kbBuilder) Build() *model.KnowledgeEntry      { return b.k }

// StoreAgentFlowMemory extracts and persists agent context memory from a run.
func StoreAgentFlowMemory(ctx context.Context, s MemoryStore, agentID, agentFlowRunID string, results map[string]any) error {
	m := NewMemory(agentID).
		Type(model.MemoryEpisodic).
		AgentFlowRunID(agentFlowRunID).
		Content(fmt.Sprintf("AgentFlow results: %v", results)).
		Metadata(map[string]any{"source": "agentflow_completion"}).
		Build()
	return s.SaveMemory(ctx, m)
}

// StoreProceduralMemory persists a learned procedural pattern.
func StoreProceduralMemory(ctx context.Context, s MemoryStore, agentID, agentFlowRunID, issue, resolution string, tags []string) error {
	m := NewMemory(agentID).
		Type(model.MemoryProcedural).
		AgentFlowRunID(agentFlowRunID).
		Content(fmt.Sprintf("Issue: %s\nResolution: %s", issue, resolution)).
		Tags(tags).
		Metadata(map[string]any{"source": "procedural_learning"}).
		Build()
	return s.SaveMemory(ctx, m)
}
