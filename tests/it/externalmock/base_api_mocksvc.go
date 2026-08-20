// Package mocksvc provides shared test infrastructure for Flowgent IT tests.
//
// Every mock in this package returns responses that match the real external API
// JSON structures 100%, so the real MCP server containers (sonarqube-mcp,
// github-mcp) can proxy and parse responses correctly. NONE of this reaches the
// network, which is exactly what makes tests/it portable to a clean CI runner.
package externalmock

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// ─── Request Recording ────────────────────────────────────────────────

// RequestRecord captures a single HTTP request for post-run verification.
type RequestRecord struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body,omitempty"`
	RequestID string            `json:"request_id"`
}

// RequestLog records requests grouped by service name.
type RequestLog struct {
	mu       sync.Mutex
	Requests map[string][]RequestRecord
}

// NewRequestLog creates a new RequestLog.
func NewRequestLog() *RequestLog {
	return &RequestLog{Requests: make(map[string][]RequestRecord)}
}

// Record adds a request to the log.
func (l *RequestLog) Record(service string, r *http.Request, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	l.Requests[service] = append(l.Requests[service], RequestRecord{
		Method:    r.Method,
		Path:      r.URL.Path + "?" + r.URL.RawQuery,
		Headers:   headers,
		Body:      body,
		RequestID: r.Header.Get("X-Request-ID"),
	})
}

// Count returns the number of recorded requests for a service.
func (l *RequestLog) Count(service string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.Requests[service])
}

// ─── LLM Call Logging ─────────────────────────────────────────────────

// LLMCallLog records each LLM request for post-run assertion.
type LLMCallLog struct {
	mu    sync.Mutex
	Calls []LLMCall
}

// LLMCall is a single captured LLM request.
type LLMCall struct {
	Model    string `json:"model"`
	System   string `json:"system"`
	User     string `json:"user"`
	Response string `json:"response"`
}

// Record adds an LLM call to the log.
func (l *LLMCallLog) Record(model, system, user, response string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Calls = append(l.Calls, LLMCall{Model: model, System: system, User: user, Response: response})
}

// Count returns the number of recorded LLM calls.
func (l *LLMCallLog) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.Calls)
}

// ─── Mock LLM (OpenAI-compatible) ─────────────────────────────────────

// NewMockLLMServer returns an httptest server that speaks the OpenAI
// ChatCompletions API. It inspects the system prompt (which the AgentExecutor
// sets to the agent's Soul) and returns deterministic JSON for each persona.
func NewMockLLMServer() *httptest.Server {
	return NewRecordingMockLLMServer(nil)
}

// NewRecordingMockLLMServer is like NewMockLLMServer but also records every
// request into the provided LLMCallLog for post-run assertion.
func NewRecordingMockLLMServer(log *LLMCallLog) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var system, user string
		for _, m := range req.Messages {
			switch m.Role {
			case "system":
				system = m.Content
			case "user":
				user = m.Content
			}
		}
		content := llmResponseFor(system)
		if log != nil {
			log.Record(req.Model, system, user, content)
		}
		resp := map[string]any{
			"id":     "cmpl-mock",
			"object": "chat.completion",
			"model":  req.Model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": content},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func llmResponseFor(system string) string {
	s := strings.ToLower(system)
	switch {
	case strings.Contains(s, "fixer"):
		return `{"patches":[{"file":"src/main/java/Login.java","diff":"- String q = \"...\"+u;\n+ PreparedStatement ps = ...;"}],"summary":"parameterized query"}`
	case strings.Contains(s, "security-reviewer") || strings.Contains(s, "security reviewer"):
		return `{"decision":true,"confidence":0.95,"risk_level":"low","reason":"parameterized query removes the SQL injection"}`
	case strings.Contains(s, "quality-reviewer") || strings.Contains(s, "quality reviewer"):
		return `{"decision":true,"confidence":0.9,"reason":"no new code smells"}`
	case strings.Contains(s, "arch-reviewer") || strings.Contains(s, "architecture"):
		return `{"decision":true,"confidence":0.88,"reason":"no architectural impact"}`
	case strings.Contains(s, "supervisor"):
		return `{"action":"continue","target":"","reason":"reviews passed"}`
	default:
		return `{"issues":[{"id":"S3649","severity":"BLOCKER","file":"src/main/java/Login.java","line":42,"message":"SQL injection"}],"resolution":"complete","report":"1 BLOCKER fixed and merged to PR","status":"ok"}`
	}
}

// ─── MCP Bridge ────────────────────────────────────────────────────────

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
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	InputSchema map[string]any                 `json:"inputSchema"`
	RestAction  string                         // REST path + method, e.g. "GET /repos/{repo}/commits"
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
		Name:        "get_commit",
		Description: "Get a commit by SHA, branch, or tag",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner":  map[string]any{"type": "string"},
				"repo":   map[string]any{"type": "string"},
				"sha":    map[string]any{"type": "string"},
				"detail": map[string]any{"type": "boolean"},
			},
		},
		RestAction: "/repos/wl4g/rengine/commits?per_page=1",
	},
	{
		Name:        "get_latest_commit",
		Description: "Get the latest commit on the default branch",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner": map[string]any{"type": "string"},
				"repo":  map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/commits?per_page=1",
	},
	{
		Name:        "create_branch",
		Description: "Create a new branch from a base ref",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner": map[string]any{"type": "string"},
				"repo":  map[string]any{"type": "string"},
				"ref":   map[string]any{"type": "string"},
				"sha":   map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/git/refs",
	},
	{
		Name:        "commit_and_push",
		Description: "Create a ref (commit) and push to a branch",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner":  map[string]any{"type": "string"},
				"repo":   map[string]any{"type": "string"},
				"branch": map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/git/refs",
	},
	{
		Name:        "push_files",
		Description: "Push files (create or update) to a branch",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner":   map[string]any{"type": "string"},
				"repo":    map[string]any{"type": "string"},
				"branch":  map[string]any{"type": "string"},
				"files":   map[string]any{"type": "array"},
				"message": map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/contents/",
	},
	{
		Name:        "create_pull_request",
		Description: "Create a pull request",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner": map[string]any{"type": "string"},
				"repo":  map[string]any{"type": "string"},
				"title": map[string]any{"type": "string"},
				"head":  map[string]any{"type": "string"},
				"base":  map[string]any{"type": "string"},
				"body":  map[string]any{"type": "string"},
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
		Name:        "add_issue_comment",
		Description: "Add a comment to an issue or pull request",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner":        map[string]any{"type": "string"},
				"repo":         map[string]any{"type": "string"},
				"issue_number": map[string]any{"type": "integer"},
				"body":         map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/issues/4/comments",
	},
	{
		Name:        "create_issue_comment",
		Description: "Add a comment to an issue or pull request (alias)",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner":        map[string]any{"type": "string"},
				"repo":         map[string]any{"type": "string"},
				"issue_number": map[string]any{"type": "integer"},
				"body":         map[string]any{"type": "string"},
			},
		},
		RestAction: "/repos/wl4g/rengine/issues/4/comments",
	},
	{
		Name:        "pull_request_read",
		Description: "Get pull request details",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner":           map[string]any{"type": "string"},
				"repo":            map[string]any{"type": "string"},
				"pull_request_id": map[string]any{"type": "integer"},
			},
		},
		RestAction: "/repos/wl4g/rengine/pulls/4",
	},
}

// ─── SonarQube MCP tool definitions ───────────────────────────────────

var SonarQubeMCPTools = []mcpToolDef{
	{
		Name:        "get_overall_issues",
		Description: "Get all open issues for a project on a branch",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"projectKey": map[string]any{"type": "string"},
				"branch":     map[string]any{"type": "string"},
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
		Name:        "get_issues",
		Description: "Get all issues for a project (alias for get_overall_issues)",
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
		Name:        "GetProjectsExportFindings",
		Description: "Export all findings of a specific project branch",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string"},
				"branch":  map[string]any{"type": "string"},
			},
		},
		RestAction: "/api/ce/component?component=rengine",
	},
	{
		Name:        "get_jobs_by_commit",
		Description: "Get project analyses (jobs) by project key",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo": map[string]any{"type": "string"},
			},
		},
		RestAction: "/api/project_analyses/search?project=rengine",
	},
}
