package externalmock

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
)

// MCPBridge is a minimal MCP JSON-RPC 2.0 HTTP server that translates
// tools/call requests to the corresponding REST mock endpoint. It replaces
// the need for Docker MCP containers in IT tests — the engine's ToolNode
// connects here exactly as it would to a real MCP server.
type MCPBridge struct {
	srv      *http.Server
	URL      string
	restURL  string
	toolDefs map[string]mcpToolDef
}

type mcpToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]any         `json:"inputSchema"`
	RestAction  string                 // REST path + method, e.g. "GET /repos/{repo}/commits"
	RestParser  func(body []byte) (any, error) // extracts useful result from mocked REST response
}

// NewMCPBridge creates an MCP HTTP server that proxies tool calls to restURL
// and listens on the given host:port. Call Close() to shut down.
func NewMCPBridge(addr, restURL string, tools []mcpToolDef) (*MCPBridge, error) {
	tm := make(map[string]mcpToolDef, len(tools))
	for _, t := range tools {
		tm[t.Name] = t
	}
	b := &MCPBridge{restURL: restURL, toolDefs: tm}
	mux := http.NewServeMux()
	mux.HandleFunc("/", b.handleMCP)
	b.srv = &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	b.URL = "http://" + ln.Addr().String()
	go b.srv.Serve(ln)
	return b, nil
}

// NewFixedPortMCPBridge is like NewMCPBridge but uses a fixed addr directly.
func NewFixedPortMCPBridge(addr, restURL string, tools []mcpToolDef) *MCPBridge {
	tm := make(map[string]mcpToolDef, len(tools))
	for _, t := range tools {
		tm[t.Name] = t
	}
	b := &MCPBridge{restURL: restURL, toolDefs: tm, URL: "http://localhost" + addr}
	mux := http.NewServeMux()
	mux.HandleFunc("/", b.handleMCP)
	b.srv = &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		panic("MCPBridge " + addr + ": " + err.Error())
	}
	go b.srv.Serve(ln)
	return b
}

func (b *MCPBridge) Close() error { return b.srv.Close() }

func (b *MCPBridge) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      any            `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeMCPError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case "initialize":
		writeMCPResult(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mock-mcp-bridge", "version": "1.0"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		tools := make([]mcpToolDef, 0, len(b.toolDefs))
		for _, t := range b.toolDefs {
			tools = append(tools, t)
		}
		writeMCPResult(w, req.ID, map[string]any{"tools": tools})
	case "tools/call":
		b.handleToolCall(w, req)
	default:
		writeMCPError(w, req.ID, -32601, "Method not found: "+req.Method)
	}
}

func (b *MCPBridge) handleToolCall(w http.ResponseWriter, req struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}) {
	name, _ := req.Params["name"].(string)
	def, ok := b.toolDefs[name]
	if !ok {
		writeMCPError(w, req.ID, -32602, "Unknown tool: "+name)
		return
	}

	resp, err := http.Get(b.restURL + def.RestAction)
	if err != nil {
		writeMCPResult(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": `{"error":"` + err.Error() + `"}`}},
			"isError": true,
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	text := string(body)
	if def.RestParser != nil {
		if parsed, err := def.RestParser(body); err == nil {
			if j, e := json.Marshal(parsed); e == nil {
				text = string(j)
			}
		}
	}
	writeMCPResult(w, req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

func writeMCPResult(w http.ResponseWriter, id any, result map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func writeMCPError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

// ─── GitHub MCP tool definitions ──────────────────────────────────────

var GitHubMCPTools = []mcpToolDef{
	{
		Name:        "get_latest_commit",
		Description: "Get the latest commit for a repository",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"repo": map[string]any{"type": "string"}}},
		RestAction:  "/repos/wl4g/rengine/commits?per_page=1",
	},
	{
		Name:        "create_branch",
		Description: "Create a new branch",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"ref": map[string]any{"type": "string"}, "sha": map[string]any{"type": "string"}}},
		RestAction:  "/repos/wl4g/rengine/git/refs",
	},
	{
		Name:        "commit_and_push",
		Description: "Commit files and push to a branch",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"branch": map[string]any{"type": "string"}}},
		RestAction:  "/repos/wl4g/rengine/contents/",
	},
	{
		Name: "create_pull_request",
		Description: "Create a pull request",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"base": map[string]any{"type": "string"},
				"head": map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/pulls",
		RestParser: func(body []byte) (any, error) {
			var pr struct {
				Number int    `json:"number"`
				URL    string `json:"html_url"`
			}
			json.Unmarshal(body, &pr)
			return pr, nil
		},
	},
	{
		Name:        "create_issue_comment",
		Description: "Create a comment on a PR",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"pr_number": map[string]any{"type": "integer"}}},
		RestAction:  "/repos/wl4g/rengine/issues/4/comments",
	},
	{
		Name:        "get_pull_request",
		Description: "Get pull request details",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"pr_number": map[string]any{"type": "integer"}}},
		RestAction:  "/repos/wl4g/rengine/pulls/4",
	},
}

// ─── SonarQube MCP tool definitions ───────────────────────────────────

var SonarQubeMCPTools = []mcpToolDef{
	{
		Name:        "get_issues",
		Description: "Search for SonarQube issues by project and severity",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_key": map[string]any{"type": "string"},
				"severities":  map[string]any{"type": "string"},
			},
		},
		RestAction: "/api/issues/search?projectKeys=rengine&severities=BLOCKER,CRITICAL,MAJOR&ps=50",
		RestParser: func(body []byte) (any, error) {
			var result struct {
				Issues []map[string]any `json:"issues"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				return nil, err
			}
			return map[string]any{"total": len(result.Issues), "issues": result.Issues}, nil
		},
	},
	{
		Name:        "get_jobs_by_commit",
		Description: "Get SonarQube background tasks for a project",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo": map[string]any{"type": "string"},
			},
		},
		RestAction: "/api/ce/component?component=rengine",
	},
}
