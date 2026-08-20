package knowledge

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IKnowledgeStore is the knowledge entry entity store interface.
type IKnowledgeStore interface {
	Get(ctx context.Context, namespace, id string) (*entities.KnowledgeEntry, error)
	List(ctx context.Context, filter ListFilter) (*entities.Page[entities.KnowledgeEntry], error)
	Save(ctx context.Context, entity *entities.KnowledgeEntry) error
	Delete(ctx context.Context, namespace, id string) error
	Search(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error)
	ListTags(ctx context.Context, namespace, scope string) ([]string, error)
}

type ListFilter struct {
	Namespace string
	Scope     string
	Tags      []string
	Page      entities.PageRequest
}
