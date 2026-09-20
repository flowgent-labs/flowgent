package skill

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ISkillStore is the skill definition entity store interface.
type ISkillStore interface {
	Get(ctx context.Context, name string) (*entities.SkillInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error)
	Save(ctx context.Context, entity *entities.SkillInfo) error
	Delete(ctx context.Context, name string) error
}
