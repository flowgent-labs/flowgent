// Package mocksvc provides shared test infrastructure for Flowgent IT tests.
//
// Every mock in this package returns responses that match the real external API
// JSON structures 100%, so the real MCP server containers (sonarqube-mcp,
// github-mcp) can proxy and parse responses correctly. NONE of this reaches the
// network, which is exactly what makes tests/it portable to a clean CI runner.
package externalmock

import (
	"encoding/json"
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

