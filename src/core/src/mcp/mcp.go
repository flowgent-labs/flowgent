package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// McpManager manages MCP client lifecycle.
type McpManager struct {
	mu      sync.Mutex
	clients map[string]*client.Client
	defs    map[string]definition
}

type definition struct {
	command []string
	args    []string
	env     map[string]string
}

// NewFactory creates an MCP client manager.
func NewMcpManager() *McpManager {
	return &McpManager{
		clients: make(map[string]*client.Client),
		defs:    make(map[string]definition),
	}
}

// Register adds an MCP server definition.
func (f *McpManager) Register(name string, command, args []string, env map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defs[name] = definition{command: command, args: args, env: env}
}

// GetClient returns a connected MCP client for the given server name.
func (f *McpManager) GetClient(ctx context.Context, name string) (*client.Client, error) {
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
		// Normalize key to uppercase (viper lowercases YAML keys)
		key := strings.ToUpper(k)
		prefix := key + "="
		found := false
		for i := 0; i < len(env); i++ {
			if len(env[i]) >= len(prefix) && strings.EqualFold(env[i][:len(prefix)], prefix) {
				if found {
					// Remove duplicate (e.g. both all_proxy and ALL_PROXY exist)
					env = append(env[:i], env[i+1:]...)
					i--
				} else {
					env[i] = prefix + v
					found = true
				}
			}
		}
		if !found {
			env = append(env, prefix+v)
		}
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
func (f *McpManager) CallTool(ctx context.Context, clientName, toolName string, args map[string]any) (map[string]any, error) {
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
