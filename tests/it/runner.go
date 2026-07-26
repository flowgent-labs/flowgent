// Package it is the integration-test entry point. It provides ITRunner — a
// harness that boots the real Flowgent apiserver + engine with local mock
// servers for external APIs. Every middleware dependency (PostgreSQL via
// Docker) is real; only external SaaS APIs (LLM, GitHub, SonarQube) are mocked.
//
// Prerequisite: docker compose -f deploy/docker/pgvector/docker-compose.yml up -d
package it

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/flowgent-labs/flowgent/api/pkg"
	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/tests/it/externalmock"
	"github.com/jackc/pgx/v5/pgxpool"
)

// logProgress writes a timestamped progress message directly to the controlling
// terminal (/dev/tty) so it appears in real-time during long-running integration
// tests. go test redirects fd 1 (stdout) and fd 2 (stderr) into internal pipes
// that are only flushed when the test process exits — meaning fmt.Fprintf(os.Stderr)
// and os.Stderr.WriteString are invisible until the test completes (or is killed).
// syscall.Write(2, ...) hits the same pipe and suffers the same fate. /dev/tty is
// NOT a file descriptor but a kernel device that always points to the real
// terminal regardless of any fd redirection — same trick used by ssh, sudo, and gpg
// when they need to read a password while stdin is piped.
func logProgress(format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("    --- %s %s\n", ts, msg)
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		tty.WriteString(line)
		tty.Close()
	}
}

// ITRunner boots the real apiserver + engine with PostgreSQL from Docker.
// Individual component tests use HTTP API for triggers and status, and raw
// SQL via Pool() for data verification.
type ITRunner struct {
	T         *testing.T
	APIURL    string
	Namespace string
	Flow      *entities.FlowInfo
	LLMLog    *externalmock.LLMCallLog

	pool *pgxpool.Pool
}

// Pool returns the raw pgxpool so tests can run verification SQL.
func (r *ITRunner) Pool() *pgxpool.Pool { return r.pool }

// ExpectTableHasRows is a convenience assertion that at least one row matches.
func (r *ITRunner) ExpectTableHasRows(table, cond string) {
	r.T.Helper()
	ctx := context.Background()
	var n int
	if err := r.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", table, cond)).Scan(&n); err != nil {
		r.T.Fatalf("query %s WHERE %s: %v", table, cond, err)
	}
	if n == 0 {
		r.T.Fatalf("table %s has 0 rows matching %q", table, cond)
	}
}

// New boots the full Flowgent stack with random-port httptest external mocks.
func New(t *testing.T, flow *entities.FlowInfo) *ITRunner {
	return NewWithLLMLog(t, flow, nil)
}

// NewWithLLMLog is like New but records every LLM call into llmLog.
func NewWithLLMLog(t *testing.T, flow *entities.FlowInfo, llmLog *externalmock.LLMCallLog) *ITRunner {
	t.Helper()
	logger := utils.NewLogger("TEXT", "ERROR")

	llmSrv := externalmock.NewRecordingMockLLMServer(llmLog)
	t.Cleanup(llmSrv.Close)

	log := externalmock.NewRequestLog()
	sqSrv := externalmock.NewMockSonarQubeAPI(log)
	ghSrv := externalmock.NewMockGitHubAPI(log)
	t.Cleanup(func() { sqSrv.Close(); ghSrv.Close() })

	return newRunner(t, flow, llmLog, logger, sqSrv.URL, ghSrv.URL, llmSrv.URL)
}

// NewWithExternalMocks is like New but also starts in-process MCP bridges
// (:13080 sonarqube-mcp, :13081 github-mcp) that translate MCP JSON-RPC
// calls to fixed-port REST mocks (:19001/:19002). No Docker required —
// the MCP bridge is a lightweight Go HTTP server running in the same process.
func NewWithExternalMocks(t *testing.T, flow *entities.FlowInfo) *ITRunner {
	t.Helper()
	logger := utils.NewLogger("TEXT", "ERROR")

	llmSrv := externalmock.NewRecordingMockLLMServer(nil)
	t.Cleanup(llmSrv.Close)

	log := externalmock.NewRequestLog()
	sqREST := externalmock.NewFixedPortSonarQubeAPI(log)
	ghREST := externalmock.NewFixedPortGitHubAPI(log)
	t.Cleanup(func() { sqREST.Close(); ghREST.Close() })

	// In-process MCP bridges: the engine's ToolNode calls these via MCP
	// protocol, and they forward to the REST mocks above.
	sqMCP := externalmock.NewFixedPortMCPBridge(":13080", "http://localhost:19001", externalmock.SonarQubeMCPTools)
	ghMCP := externalmock.NewFixedPortMCPBridge(":13081", "http://localhost:19002", externalmock.GitHubMCPTools)
	t.Cleanup(func() { sqMCP.Close(); ghMCP.Close() })

	return newRunner(t, flow, nil, logger, "http://localhost:13080", "http://localhost:13081", llmSrv.URL)
}

func pgHostPort() (string, string) {
	host := os.Getenv("FLOWGENT_TEST_PG_HOST")
	port := os.Getenv("FLOWGENT_TEST_PG_PORT")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "15432"
	}
	return host, port
}

func newRunner(t *testing.T, flow *entities.FlowInfo, llmLog *externalmock.LLMCallLog, logger *utils.Logger,
	sqURL, ghURL, llmURL string) *ITRunner {

	t.Helper()
	namespace := "test"
	flow.Namespace = namespace

	host, port := pgHostPort()
	cfg := &config.FlowgentConfig{
		Runtime:       config.RuntimeConfig{Namespace: config.NamespaceConfig{DefaultNamespace: namespace, NamespacePrefix: "flowgent-"}},
		Orchestration: config.OrchestrationConfig{MaxConcurrentFlows: 8, FlowExecutionTimeout: "120s", MaxNodeRetries: 2},
	}
	cfg.Storage.Type = "POSTGRE"
	cfg.Storage.Postgres = config.PostgresConfig{
		Host:           host,
		Port:           atoi(port),
		Database:       "flowgent_test",
		Schema:         "public",
		Username:       "flowgent",
		Password:       "flowgent",
		MinConnections: 2,
		MaxConnections: 8,
	}

	logProgress("[store] connecting to PostgreSQL at %s:%s", host, port)
	storeImpl := storepkg.InitStore(cfg)
	t.Cleanup(func() { _ = storeImpl.Close() })

	pool, ok := storeImpl.DB().(*pgxpool.Pool)
	if !ok {
		t.Fatalf("store.DB() is %T, want *pgxpool.Pool — is PostgreSQL running? (cd deploy/docker/pgvector && docker compose up -d)", storeImpl.DB())
	}

	// ── apiserver (no auth) ──
	flowHandler := handler.NewFlowDefHandler(storeImpl, logger, []entities.FlowInfo{*flow}, map[string]entities.FlowInfo{}, "flowgent-", namespace, nil)
	nw := handler.NewNotifierWSBridge(nil)
	restMux := api.RegisterRESTRoutes(
		&handler.HealthHandler{},
		flowHandler,
		handler.NewAgentDefHandler(storeImpl, logger),
		handler.NewFlowRunHandler(storeImpl, nil, logger),
		handler.NewHumanHandler(storeImpl, nil, logger),
		handler.NewNotifierHandler(storeImpl, logger),
		nw,
		handler.NewLlmProviderHandler(storeImpl),
		handler.NewMcpHandler(storeImpl),
		handler.NewWebhookHandler(flowHandler, logger, namespace),
		handler.NewKnowledgeHandler(storeImpl),
	)
	srv := httptest.NewServer(restMux)
	t.Cleanup(srv.Close)

	r := &ITRunner{T: t, APIURL: srv.URL, Namespace: namespace, Flow: flow, LLMLog: llmLog, pool: pool}

	logProgress("[seed] registering agents, LLM provider, and MCP servers")
	r.SeedAgents()
	r.SeedLLMProvider(llmURL)
	r.SeedMCP("github", ghURL)
	r.SeedMCP("sonarqube", sqURL)

	// ── engine ──
	logProgress("[engine] starting resource manager and job manager")
	apiClient := client.NewFlowgentClient(srv.URL)
	rm, err := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:     engine.ProviderStandalone,
		PoolSize:     8,
		TaskState:    &client.TaskStateClient{Client: apiClient, Namespace: namespace},
		ApprovalInfo: &client.HumanApprovalClient{Client: apiClient},
		Logger:       logger,
		APIServerURL: srv.URL,
		Namespace:    namespace,
	})
	if err != nil {
		t.Fatalf("create resource manager: %v", err)
	}
	jm, err := jobmanager.NewJobManager(
		&client.RunStateClient{Client: apiClient, Namespace: namespace},
		rm, logger,
		&jobmanager.JobManagerConfig{FlowExecutionTimeout: 120 * time.Second, MaxNodeRetries: 2, MaxConcurrentFlows: 8},
	)
	if err != nil {
		t.Fatalf("create job manager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go jobmanager.StartRunPoller(ctx, apiClient, namespace, jm, flowHandler.AgentFlows(), "", "")

	logProgress("[runner] stack ready at %s", srv.URL)
	return r
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// ─── seeding ─────────────────────────────────────────────────────────────────

func (r *ITRunner) Post(path string, body any) {
	r.T.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(r.APIURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		r.T.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		r.T.Fatalf("POST %s -> %d: %s", path, resp.StatusCode, msg)
	}
}

func (r *ITRunner) SeedAgents() {
	agents := []entities.AgentInfo{
		{Name: "issue-detector", Model: "mock/echo", Soul: "You are a DevSecOps issue-detector."},
		{Name: "fixer-agent", Model: "mock/echo", Soul: "You are a secure-coding fixer agent."},
		{Name: "security-reviewer", Model: "mock/echo", Soul: "You are a strict security-reviewer."},
		{Name: "quality-reviewer", Model: "mock/echo", Soul: "You are a code quality-reviewer."},
		{Name: "arch-reviewer", Model: "mock/echo", Soul: "You are an architecture reviewer."},
	}
	for _, a := range agents {
		r.Post("/api/v1/"+r.Namespace+"/agents", a)
	}
}

func (r *ITRunner) SeedLLMProvider(endpoint string) {
	// Clean up stale mock providers from previous test runs so the engine
	// doesn't pick up a URL whose httptest server has already been closed.
	_, _ = r.pool.Exec(context.Background(), `DELETE FROM llm_providers WHERE provider = 'mock'`)
	r.Post("/api/v1/"+r.Namespace+"/llm/providers", entities.LlmProviderInfo{
		Provider: "mock", Endpoint: endpoint, ApiKey: "test-key",
		Status: "ACTIVE", Enabled: true, RateLimit: 100000,
	})
}

func (r *ITRunner) SeedMCP(name, url string) {
	_, _ = r.pool.Exec(context.Background(), `DELETE FROM llm_mcp WHERE name = $1 AND namespace_id = $2`, name, r.Namespace)
	r.Post("/api/v1/"+r.Namespace+"/mcp", entities.McpInfo{
		Name: name, Type: "http", URL: url, Enabled: true,
	})
}

// ─── trigger + poll ──────────────────────────────────────────────────────────

func (r *ITRunner) TriggerManual(vars map[string]any) []string {
	r.T.Helper()
	payload := map[string]any{"vars": vars}
	if payload["vars"] == nil {
		payload["vars"] = map[string]any{}
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(r.APIURL+"/api/v1/"+r.Namespace+"/flows/"+r.Flow.ID+"/trigger", "application/json", bytes.NewReader(b))
	if err != nil {
		r.T.Fatalf("trigger POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		r.T.Fatalf("trigger -> %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		RunID string `json:"run_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.RunID == "" {
		r.T.Fatalf("trigger: no run_id in response")
	}
	logProgress("[trigger] flow %s → run %s", r.Flow.ID, out.RunID)
	return []string{out.RunID}
}

func (r *ITRunner) TriggerGitHubPR(prNumber int, commitSHA string) []string {
	return r.TriggerGitHubEvent("pull_request", prNumber, commitSHA)
}

func (r *ITRunner) TriggerGitHubEvent(event string, prNumber int, commitSHA string) []string {
	r.T.Helper()
	payload := map[string]any{
		"action":     "opened",
		"number":     prNumber,
		"repository": map[string]any{"full_name": "wl4g/rengine"},
		"sender":     map[string]any{"login": "flowgent-bot"},
	}
	if event == "pull_request" {
		payload["pull_request"] = map[string]any{
			"number": prNumber,
			"head":   map[string]any{"ref": "fix/flowgent_sec_auto_fix", "sha": commitSHA},
		}
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, r.APIURL+"/api/v1/webhook/github", bytes.NewReader(b))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.T.Fatalf("webhook POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		r.T.Fatalf("webhook -> %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		Triggered []string `json:"triggered"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	logProgress("[trigger] webhook %s PR#%d → %d run(s)", event, prNumber, len(out.Triggered))
	return out.Triggered
}

func (r *ITRunner) WaitRun(runID string, timeout time.Duration) string {
	r.T.Helper()
	logProgress("[wait] polling run %s (timeout %s)", runID, timeout)
	deadline := time.Now().Add(timeout)
	var status string
	pollCount := 0
	for time.Now().Before(deadline) {
		status = r.RunStatus(runID)
		if status == string(entities.RunCompleted) || status == string(entities.RunFailed) {
			logProgress("[wait] run %s finished: %s", runID, status)
			return status
		}
		pollCount++
		if pollCount%25 == 0 {
			elapsed := time.Since(deadline.Add(-timeout)).Round(time.Second)
			logProgress("[wait] run %s still %s (elapsed %s)", runID, status, elapsed)
		}
		time.Sleep(200 * time.Millisecond)
	}
	logProgress("[wait] run %s timed out after %s (last status: %s)", runID, timeout, status)
	return status
}

func (r *ITRunner) RunStatus(runID string) string {
	resp, err := http.Get(r.APIURL + "/api/v1/" + r.Namespace + "/runs/" + runID)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var run struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&run)
	return run.Status
}


