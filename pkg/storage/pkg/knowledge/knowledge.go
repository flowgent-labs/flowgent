package knowledge

import (
	"context"
	"errors"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

var (
	ErrApprovalExpired    = errors.New("publication approval expired")
	ErrPublicationStale   = errors.New("publication baseline changed; a new approval is required")
	ErrPublicationDenied  = errors.New("publication target is not authorized")
	ErrCandidateImmutable = errors.New("candidate idempotency key already represents different content")
)

// IKnowledgeStore is the knowledge entry entity store interface.
type IKnowledgeStore interface {
	Get(ctx context.Context, namespace, id string) (*entities.KnowledgeEntry, error)
	List(ctx context.Context, filter ListFilter) (*entities.Page[entities.KnowledgeEntry], error)
	Save(ctx context.Context, entity *entities.KnowledgeEntry) error
	Delete(ctx context.Context, namespace, id string) error
	Search(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error)
	ListTags(ctx context.Context, namespace, scope string) ([]string, error)
	CreateEmbeddingProfile(ctx context.Context, profile *entities.EmbeddingProfile) error
	PutEmbedding(ctx context.Context, embedding *entities.KnowledgeEmbedding) error
	CreateCandidate(ctx context.Context, candidate *entities.KnowledgeCandidate) (*entities.ApprovalInfo, error)
	ResolveCandidateApproval(ctx context.Context, approvalID string, approved bool, actor string, decision map[string]any) (*entities.KnowledgeCandidate, error)
}

type ListFilter struct {
	Namespace string
	Scope     string
	Tags      []string
	Page      entities.PageRequest
}
