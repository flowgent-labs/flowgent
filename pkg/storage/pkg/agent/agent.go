package agent

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type agentRecord struct {
	Name         string         `db:"name"`
	Revision     int64          `db:"revision"`
	Soul         string         `db:"soul"`
	Instruction  string         `db:"instruction"`
	ModelConfig  map[string]any `db:"model_config"`
	InputSchema  map[string]any `db:"input_schema"`
	OutputSchema map[string]any `db:"output_schema"`
	ID           string         `db:"id"`
	Description  string         `db:"description"`
	Namespace    string         `db:"namespace_id"`
	Status       string         `db:"status"`
	CreatedAt    time.Time      `db:"created_at"`
	CreatedBy    string         `db:"created_by"`
	UpdatedAt    time.Time      `db:"updated_at"`
	UpdatedBy    string         `db:"updated_by"`
	RowVersion   int64          `db:"row_version"`
	Metadata     map[string]any `db:"metadata"`
}

func (r *agentRecord) entity() *entities.AgentInfo {
	item := &entities.AgentInfo{
		BaseEntity: entities.BaseEntity{ID: r.ID, Description: r.Description, Namespace: r.Namespace,
			Status: r.Status, CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, UpdatedAt: r.UpdatedAt,
			UpdatedBy: r.UpdatedBy, RowVersion: r.RowVersion, Metadata: r.Metadata},
		Name: r.Name, Soul: r.Soul, Instruction: r.Instruction, InputSchema: r.InputSchema,
		OutputSchema: r.OutputSchema, Revision: r.Revision, Version: r.Revision,
	}
	if model, ok := r.ModelConfig["model"].(string); ok {
		item.Model = model
	}
	if value, ok := r.ModelConfig["temperature"].(float64); ok {
		item.Temperature = &value
	}
	if value, ok := r.ModelConfig["max_tokens"].(float64); ok {
		item.MaxTokens = int(value)
	}
	return item
}

func agentModelConfig(item *entities.AgentInfo) map[string]any {
	result := map[string]any{"model": item.Model}
	if item.Temperature != nil {
		result["temperature"] = *item.Temperature
	}
	if item.MaxTokens > 0 {
		result["max_tokens"] = item.MaxTokens
	}
	return result
}

// IAgentInfoStore is the agent info entity store interface.
type IAgentInfoStore interface {
	Get(ctx context.Context, namespace, name string) (*entities.AgentInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error)
	Save(ctx context.Context, entity *entities.AgentInfo) error
	Delete(ctx context.Context, namespace, name string) error
}
