package mcp

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IMCPStore is the MCP definition entity store interface.
type IMCPStore interface {
	Get(ctx context.Context, namespace, name string) (*entities.McpInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.McpInfo], error)
	Save(ctx context.Context, entity *entities.McpInfo) error
	Delete(ctx context.Context, namespace, name string) error
}
