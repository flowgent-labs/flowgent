package skill

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ISkillStore is the skill definition entity store interface.
type ISkillStore interface {
	Get(ctx context.Context, namespace, name string) (*entities.SkillInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error)
	Save(ctx context.Context, entity *entities.SkillInfo) error
	Delete(ctx context.Context, namespace, name string) error
}
