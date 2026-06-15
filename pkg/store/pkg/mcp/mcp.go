package mcp

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// IMCPStore is the MCP definition entity store interface.
type IMCPStore interface {
	Get(ctx context.Context, name string) (*entities.McpInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.McpInfo], error)
	Save(ctx context.Context, entity *entities.McpInfo) error
	Delete(ctx context.Context, name string) error
}
