package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// McpManager manages MCP HTTP client lifecycle. TM pods are backend agents
// in K8s — no human interaction, no stdio subprocess. All MCP servers are
// accessed via Streamable HTTP transport.
type McpManager struct {
	mu         sync.Mutex
	clients    map[string]*client.Client
	defs       map[string]httpDef
	httpClient model.IFlowgentAPIClient
}

type httpDef struct {
	url     string
	headers map[string]string
}

func NewMcpManager(httpClient model.IFlowgentAPIClient) *McpManager {
	return &McpManager{
		clients:    make(map[string]*client.Client),
		defs:       make(map[string]httpDef),
		httpClient: httpClient,
	}
}

func (f *McpManager) HttpClient() model.IFlowgentAPIClient {
	return f.httpClient
}

// Register adds an HTTP MCP server definition.
func (f *McpManager) Register(name, url string, headers map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defs[name] = httpDef{url: url, headers: headers}
}

// GetClient returns a connected MCP HTTP client for the given server name.
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

	opts := []transport.StreamableHTTPCOption{}
	if len(def.headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(def.headers))
	}

	c, err := client.NewStreamableHttpClient(def.url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect MCP %s at %s: %w", name, def.url, err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return nil, fmt.Errorf("init MCP %s at %s: %w", name, def.url, err)
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
