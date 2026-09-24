package skill

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type skillRecord struct {
	Name        string         `db:"name"`
	Revision    int64          `db:"revision"`
	Instruction string         `db:"instruction"`
	ModelConfig map[string]any `db:"model_config"`
	Tools       []string       `db:"tools"`
	ID          string         `db:"id"`
	Description string         `db:"description"`
	Namespace   string         `db:"namespace_id"`
	Status      string         `db:"status"`
	CreatedAt   time.Time      `db:"created_at"`
	CreatedBy   string         `db:"created_by"`
	UpdatedAt   time.Time      `db:"updated_at"`
	UpdatedBy   string         `db:"updated_by"`
	RowVersion  int64          `db:"row_version"`
	Metadata    map[string]any `db:"metadata"`
}

type skillFileRecord struct {
	ID           string         `db:"id"`
	Kind         string         `db:"kind"`
	RelativePath string         `db:"relative_path"`
	MediaType    string         `db:"media_type"`
	SizeBytes    int64          `db:"size_bytes"`
	ContentHash  string         `db:"content_hash"`
	CreatedAt    time.Time      `db:"created_at"`
	CreatedBy    string         `db:"created_by"`
	Metadata     map[string]any `db:"metadata"`
}

func (r *skillFileRecord) entity() entities.SkillFile {
	return entities.SkillFile{
		ID:           r.ID,
		Kind:         r.Kind,
		RelativePath: r.RelativePath,
		MediaType:    r.MediaType,
		SizeBytes:    r.SizeBytes,
		ContentHash:  r.ContentHash,
		CreatedAt:    r.CreatedAt,
		CreatedBy:    r.CreatedBy,
		Metadata:     r.Metadata,
	}
}

func (r *skillRecord) entity() *entities.SkillInfo {
	item := &entities.SkillInfo{BaseEntity: entities.BaseEntity{ID: r.ID, Description: r.Description, Namespace: r.Namespace, Status: r.Status, CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy, RowVersion: r.RowVersion, Metadata: r.Metadata}, Name: r.Name, Instruction: r.Instruction, Tools: r.Tools, Revision: r.Revision, Version: r.Revision}
	if value, ok := r.ModelConfig["model"].(string); ok {
		item.Model = value
	}
	if value, ok := r.ModelConfig["temperature"].(float64); ok {
		item.Temperature = &value
	}
	if value, ok := r.ModelConfig["max_tokens"].(float64); ok {
		item.MaxTokens = int(value)
	}
	return item
}

func skillModelConfig(item *entities.SkillInfo) map[string]any {
	result := map[string]any{"model": item.Model}
	if item.Temperature != nil {
		result["temperature"] = *item.Temperature
	}
	if item.MaxTokens > 0 {
		result["max_tokens"] = item.MaxTokens
	}
	return result
}

func skillTools(item *entities.SkillInfo) []string {
	if item.Tools == nil {
		return []string{}
	}
	return item.Tools
}

// ISkillStore is the skill definition entity store interface.
type ISkillStore interface {
	Get(ctx context.Context, namespace, name string) (*entities.SkillInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error)
	ListFiles(ctx context.Context, namespace, name string) ([]entities.SkillFile, error)
	Save(ctx context.Context, entity *entities.SkillInfo) error
	SaveFileRevision(ctx context.Context, namespace, name string, file entities.SkillFile, principal string) (*entities.SkillInfo, error)
	Delete(ctx context.Context, namespace, name string) error
}
