package knowledge

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IKnowledgeStore is the knowledge entry entity store interface.
type IKnowledgeStore interface {
	// Standard CRUD (delegated to generic store)
	Get(ctx context.Context, id string) (*entities.KnowledgeEntry, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.KnowledgeEntry], error)
	Save(ctx context.Context, entity *entities.KnowledgeEntry) error
	Delete(ctx context.Context, id string) error

	// Search performs keyword-based search on title/content with optional tag filter.
	Search(ctx context.Context, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error)

	// SearchByTags returns entries matching any of the given tags.
	SearchByTags(ctx context.Context, tags []string) ([]*entities.KnowledgeEntry, error)

	// UpsertBySourceRef creates or updates an entry matched by source reference.
	UpsertBySourceRef(ctx context.Context, entity *entities.KnowledgeEntry) error

	// ListTags returns all distinct tag values across non-deleted entries.
	ListTags(ctx context.Context) ([]string, error)
}
