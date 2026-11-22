package agentgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

var ErrAlreadyExists = errors.New("graph already exists")

// IAgentGraphStore is the graph info entity store interface.
type IAgentGraphStore interface {
	Get(ctx context.Context, namespace, id string) (*entities.AgentGraphRevision, error)
	Select(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentGraphRevision], error)
	Delete(ctx context.Context, namespace, id string) error
	GetVersion(ctx context.Context, namespace, id string, version int64) (*entities.AgentGraphRevision, error)
	CreateSpec(ctx context.Context, spec *entities.AgentGraph, createdBy, comment string) error
	SaveSpec(ctx context.Context, spec *entities.AgentGraph, createdBy, comment string) error
	GetSpec(ctx context.Context, namespace, id string) (*entities.AgentGraph, error)
}

func marshalSpec(spec *entities.AgentGraph) ([]byte, error) {
	if spec.Namespace == "" {
		spec.Namespace = defaultNamespace
	}
	if spec.Kind == "" || strings.EqualFold(spec.Kind, "graph") {
		if err := resourceid.Validate(spec.ResourceName()); err != nil {
			return nil, fmt.Errorf("invalid graph name: %w", err)
		}
	}
	persisted := *spec
	persisted.Revision = 0
	b, err := json.Marshal(&persisted)
	if err != nil {
		return nil, fmt.Errorf("marshal graph definition: %w", err)
	}
	return b, nil
}

func hydrateSpec(spec *entities.AgentGraph, version *entities.AgentGraphRevision) {
	spec.ID = version.AgentGraphID
	spec.Name = version.AgentGraphName
	spec.Revision = version.Revision
	spec.SummarizeEnabled = version.SummarizeEnabled
	spec.Namespace = version.Namespace
	spec.Status = version.Status
	spec.CreatedAt = version.CreatedAt
	spec.CreatedBy = version.CreatedBy
	spec.UpdatedAt = version.UpdatedAt
	spec.UpdatedBy = version.UpdatedBy
	spec.RowVersion = version.RowVersion
	spec.Metadata = version.Metadata
}
