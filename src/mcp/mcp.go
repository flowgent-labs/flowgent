package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// Factory manages MCP client lifecycle.
type Factory struct {
	mu      sync.Mutex
	clients map[string]*client.Client
	defs    map[string]definition
}

type definition struct {
	command []string
	args    []string
	env     map[string]string
}

// NewFactory creates an MCP client factory.
func NewFactory() *Factory {
	return &Factory{
		clients: make(map[string]*client.Client),
		defs:    make(map[string]definition),
	}
}

// Register adds an MCP server definition.
func (f *Factory) Register(name string, command, args []string, env map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defs[name] = definition{command: command, args: args, env: env}
}

// GetClient returns a connected MCP client for the given server name.
func (f *Factory) GetClient(ctx context.Context, name string) (*client.Client, error) {
	f.mu.Lock()
	if c, ok := f.clients[name]; ok {
		f.mu.Unlock()
		return c, nil
	}
	def, ok := f.defs[name]
	if !ok {
		f.mu.Unlock()
		return nil, fmt.Errorf("MCP not found: %s", name)
	}
	f.mu.Unlock()

	env := os.Environ()
	for k, v := range def.env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

		allArgs := make([]string, 0, len(def.command)-1+len(def.args))
		allArgs = append(allArgs, def.command[1:]...)
		allArgs = append(allArgs, def.args...)
	c, err := client.NewStdioMCPClient(def.command[0], env, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("start MCP %s: %w", name, err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return nil, fmt.Errorf("init MCP %s: %w", name, err)
	}

	f.mu.Lock()
	f.clients[name] = c
	f.mu.Unlock()
	return c, nil
}

// CallTool invokes a tool on the named MCP server.
func (f *Factory) CallTool(ctx context.Context, clientName, toolName string, args map[string]any) (map[string]any, error) {
	c, err := f.GetClient(ctx, clientName)
	if err != nil {
		return nil, err
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	result, err := c.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("call tool %s: %w", toolName, err)
	}

	output := make(map[string]any)
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			output["text"] = textContent.Text
		}
	}
	output["tool"] = toolName
	return output, nil
}
