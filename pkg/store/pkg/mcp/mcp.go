package mcp

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// IMCPStore is the MCP definition entity store interface.
type IMCPStore interface {
	Get(ctx context.Context, name string) (*model.MCPDef, error)
	Select(ctx context.Context, req model.PageRequest) (*model.Page[model.MCPDef], error)
	Save(ctx context.Context, entity *model.MCPDef) error
	Delete(ctx context.Context, name string) error
}
