package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

var ErrAlreadyExists = errors.New("flow already exists")

// IFlowInfoStore is the flow info entity store interface.
type IFlowInfoStore interface {
	Get(ctx context.Context, namespace, id string) (*entities.FlowVersionInfo, error)
	Select(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error)
	Delete(ctx context.Context, namespace, id string) error
	GetVersion(ctx context.Context, namespace, id string, version int64) (*entities.FlowVersionInfo, error)
	CreateSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error
	SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error
	GetSpec(ctx context.Context, namespace, id string) (*entities.FlowInfo, error)
}

func marshalSpec(spec *entities.FlowInfo) ([]byte, error) {
	if spec.Namespace == "" {
		spec.Namespace = defaultNamespace
	}
	if spec.Kind == "" || strings.EqualFold(spec.Kind, "flow") {
		if err := resourceid.Validate(spec.ID); err != nil {
			return nil, fmt.Errorf("invalid flow name: %w", err)
		}
	}
	persisted := *spec
	persisted.Version = 0
	b, err := json.Marshal(&persisted)
	if err != nil {
		return nil, fmt.Errorf("marshal flow definition: %w", err)
	}
	return b, nil
}

func hydrateSpec(spec *entities.FlowInfo, version *entities.FlowVersionInfo) {
	spec.Version = version.Version
	spec.Namespace = version.Namespace
	spec.Status = version.Status
	spec.CreatedAt = version.CreatedAt
	spec.CreatedBy = version.CreatedBy
	spec.UpdatedAt = version.UpdatedAt
	spec.UpdatedBy = version.UpdatedBy
	spec.DelFlag = version.DelFlag
}
