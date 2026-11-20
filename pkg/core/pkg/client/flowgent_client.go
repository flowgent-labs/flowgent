package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// FlowgentClient provides HTTP access to the API Server. It is the canonical
// state client for all non-apiserver components (controller, JM, TM, sandbox,
// notifier). Every method calls the apiserver REST API; no direct DB or cache
// access is embedded here.

type FlowgentClient struct {
	BaseURL string
}

func NewFlowgentClient(baseURL string) *FlowgentClient {
	if baseURL == "" {
		baseURL = "http://flowgent-apiserver:9990"
	}
	slog.Info("api client using apiserver", "url", baseURL)
	return &FlowgentClient{BaseURL: baseURL}
}

// ─── helpers ─────────────────────────────────────────────────────

func (c *FlowgentClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

func readJSON(resp *http.Response, into any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver returned %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// ─── Agents ──────────────────────────────────────────────────────

// ListAgents returns all agent definitions for a namespace.
func (c *FlowgentClient) ListAgents(ctx context.Context, namespace string) ([]*entities.AgentInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/agents", nil)
	if err != nil {
		return nil, fmt.Errorf("ListAgents: %w", err)
	}
	var items []*entities.AgentInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListAgents: %w", err)
	}
	return items, nil
}

// GetAgent returns a single agent definition.
func (c *FlowgentClient) GetAgent(ctx context.Context, namespace, name string) (*entities.AgentInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/agents/"+name, nil)
	if err != nil {
		return nil, fmt.Errorf("GetAgent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetAgent %d: %s", resp.StatusCode, string(b))
	}
	var item entities.AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("GetAgent: %w", err)
	}
	return &item, nil
}

// ─── AgentGraph CRUD ──────────────────────────────────────────────

func (c *FlowgentClient) ListAgentGraphs(ctx context.Context, namespace string) ([]entities.AgentGraph, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/agent-graphs", nil)
	if err != nil {
		return nil, fmt.Errorf("ListAgentGraphs: %w", err)
	}
	var items []entities.AgentGraph
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListAgentGraphs: %w", err)
	}
	return items, nil
}

func (c *FlowgentClient) GetAgentGraph(ctx context.Context, namespace, graphID string) (*entities.AgentGraph, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/agent-graphs/"+graphID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetAgentGraph: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetAgentGraph %d: %s", resp.StatusCode, string(b))
	}
	var spec entities.AgentGraph
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// ResolveGraphRuntimeConfig returns the effective namespace→AgentGraph runtime
// configuration for the controller. This endpoint is intentionally unavailable
// to human/API-key roles because it contains plaintext secret values.
func (c *FlowgentClient) ResolveGraphRuntimeConfig(ctx context.Context, namespace, graphID string) (*entities.ResolvedRuntimeConfig, error) {
	path := "/api/v1/" + url.PathEscape(namespace) + "/agent-graphs/" + url.PathEscape(graphID) + "/runtime-config/resolved"
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("ResolveGraphRuntimeConfig: %w", err)
	}
	var resolved entities.ResolvedRuntimeConfig
	if err := readJSON(resp, &resolved); err != nil {
		return nil, fmt.Errorf("ResolveGraphRuntimeConfig: %w", err)
	}
	if resolved.Environment == nil {
		resolved.Environment = map[string]string{}
	}
	if resolved.Secrets == nil {
		resolved.Secrets = map[string]string{}
	}
	return &resolved, nil
}

func (c *FlowgentClient) CreateAgentGraph(ctx context.Context, namespace string, spec *entities.AgentGraph) error {
	b, _ := json.Marshal(spec)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/agent-graphs", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("CreateAgentGraph: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateAgentGraph %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

func (c *FlowgentClient) UpdateAgentGraph(ctx context.Context, namespace, graphID string, spec *entities.AgentGraph) error {
	b, _ := json.Marshal(spec)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/agent-graphs/"+graphID, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("UpdateAgentGraph: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UpdateAgentGraph %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

func (c *FlowgentClient) DeleteAgentGraph(ctx context.Context, namespace, graphID string) error {
	resp, err := c.do(ctx, "DELETE", "/api/v1/"+namespace+"/agent-graphs/"+graphID, nil)
	if err != nil {
		return fmt.Errorf("DeleteAgentGraph: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DeleteAgentGraph %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ─── GraphRun ─────────────────────────────────────────────────────

// CreateGraphRun creates a run directly with explicit namespace, namespace, and trigger info.
func (c *FlowgentClient) CreateGraphRun(ctx context.Context, namespace string, run *entities.GraphRun) (*entities.GraphRun, error) {
	b, _ := json.Marshal(run)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/graph-runs", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("CreateGraphRun: %w", err)
	}
	var created entities.GraphRun
	if err := readJSON(resp, &created); err != nil {
		return nil, fmt.Errorf("CreateGraphRun: %w", err)
	}
	created.Normalize()
	return &created, nil
}

// TriggerRunResult is the narrow response contract of POST /agent-graphs/{name}/trigger.
// The trigger endpoint intentionally returns an acknowledgement rather than a
// full GraphRun; callers that need the run body can subsequently call
// GetGraphRun with GraphRunID.
type TriggerRunResult struct {
	AgentGraphName string                  `json:"agent_graph_name"`
	GraphRunID     string                  `json:"graph_run_id"`
	Status         entities.GraphRunStatus `json:"status"`
	Namespace      string                  `json:"namespace"`
	AgentGraphID   string                  `json:"agent_graph_id"`
}

// TriggerRun creates a PENDING run via the trigger endpoint.
func (c *FlowgentClient) TriggerRun(ctx context.Context, namespace, graphName string, input map[string]any, trigger entities.TriggerInfo) (*TriggerRunResult, error) {
	payload := map[string]any{"input": input, "trigger": trigger}
	b, _ := json.Marshal(payload)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/agent-graphs/"+url.PathEscape(graphName)+"/trigger", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("TriggerRun: %w", err)
	}
	var out TriggerRunResult
	if err := readJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("TriggerRun: %w", err)
	}
	slog.Debug("api client TriggerRun", "namespace", namespace, "graph", graphName)
	return &out, nil
}

// GetGraphRun returns a run by namespace and ID.
func (c *FlowgentClient) GetGraphRun(ctx context.Context, namespace, runID string) (*entities.GraphRun, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/graph-runs/"+runID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetGraphRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetGraphRun %d: %s", resp.StatusCode, string(b))
	}
	var run entities.GraphRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, err
	}
	run.Normalize()
	return &run, nil
}

// ListGraphRuns returns runs with optional filters. status, runtimeMode, k8sNamespace, and graphID may be empty.
func (c *FlowgentClient) ListGraphRuns(ctx context.Context, namespace, status, runtimeMode, k8sNamespace, graphID string, page, size int) (*entities.Page[entities.GraphRun], error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if runtimeMode != "" {
		q.Set("runtime_mode", runtimeMode)
	}
	if k8sNamespace != "" {
		q.Set("k8s_namespace", k8sNamespace)
	}
	if graphID != "" {
		q.Set("agent_graph_id", graphID)
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	raw := "/api/v1/" + namespace + "/graph-runs?" + q.Encode()
	resp, err := c.do(ctx, "GET", raw, nil)
	if err != nil {
		return nil, fmt.Errorf("ListGraphRuns: %w", err)
	}
	var pageResp entities.Page[entities.GraphRun]
	if err := readJSON(resp, &pageResp); err != nil {
		return nil, fmt.Errorf("ListGraphRuns: %w", err)
	}
	for _, run := range pageResp.Items {
		if run != nil {
			run.Normalize()
		}
	}
	return &pageResp, nil
}

// UpdateRunLifecycle persists the lifecycle values produced by JobMaster.
func (c *FlowgentClient) UpdateRunLifecycle(ctx context.Context, namespace, runID string, update entities.GraphRunLifecycleUpdate) error {
	b, _ := json.Marshal(update)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/graph-runs/"+runID, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("UpdateRunLifecycle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UpdateRunLifecycle %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// CancelGraphRun cancels a run.
func (c *FlowgentClient) CancelGraphRun(ctx context.Context, namespace, runID string) error {
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/graph-runs/"+runID+"/cancel", nil)
	if err != nil {
		return fmt.Errorf("CancelGraphRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CancelGraphRun %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ─── NodeRuns ────────────────────────────────────────────────────

// CreateNodeRun persists a new noderun run.
func (c *FlowgentClient) CreateNodeRun(ctx context.Context, namespace, runID string, noderun *entities.NodeRun) error {
	if noderun == nil {
		return fmt.Errorf("CreateNodeRun: noderun is required")
	}
	noderun.Normalize()
	b, err := json.Marshal(noderun)
	if err != nil {
		return fmt.Errorf("CreateNodeRun: encode noderun: %w", err)
	}
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/graph-runs/"+runID+"/node-runs", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("CreateNodeRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateNodeRun %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// UpdateNodeRun updates an existing noderun run (status, output, error).
func (c *FlowgentClient) UpdateNodeRun(ctx context.Context, namespace, runID, taskID string, noderun *entities.NodeRun) error {
	if noderun == nil {
		return fmt.Errorf("UpdateNodeRun: noderun is required")
	}
	noderun.Normalize()
	b, err := json.Marshal(noderun)
	if err != nil {
		return fmt.Errorf("UpdateNodeRun: encode noderun: %w", err)
	}
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/graph-runs/"+runID+"/node-runs/"+taskID, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("UpdateNodeRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UpdateNodeRun %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ListNodeRuns returns all tasks for a run.
func (c *FlowgentClient) ListNodeRuns(ctx context.Context, namespace, runID string) ([]*entities.NodeRun, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/graph-runs/"+runID+"/node-runs", nil)
	if err != nil {
		return nil, fmt.Errorf("ListNodeRuns: %w", err)
	}
	var items []*entities.NodeRun
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListNodeRuns: %w", err)
	}
	for _, item := range items {
		if item != nil {
			item.Normalize()
		}
	}
	return items, nil
}

// GetNodeRun returns a single noderun run.
func (c *FlowgentClient) GetNodeRun(ctx context.Context, namespace, runID, taskID string) (*entities.NodeRun, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/graph-runs/"+runID+"/node-runs/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetNodeRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetNodeRun %d: %s", resp.StatusCode, string(b))
	}
	var item entities.NodeRun
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	item.Normalize()
	return &item, nil
}

// SavePlan creates/updates the execution plan state via apiserver.
func (c *FlowgentClient) SavePlan(ctx context.Context, namespace, runID string, plan *entities.ExecutionPlan) error {
	// execution plans graph through the tasks endpoint
	noderun := &entities.NodeRun{
		BaseEntity:     entities.BaseEntity{ID: plan.NodeRunID},
		GraphRunID:     plan.GraphRunID,
		NodeInstanceID: plan.NodeInstanceID,
		Attempt:        plan.RetryCount + 1,
		Status:         plan.State,
		Input:          plan.Input,
		ExecutionID:    plan.PlanID,
	}
	return c.CreateNodeRun(ctx, namespace, runID, noderun)
}

// ─── Human Approvals ─────────────────────────────────────────────

// CreateApproval creates a pending human approval.
func (c *FlowgentClient) CreateApproval(ctx context.Context, namespace string, approval *entities.ApprovalInfo) error {
	b, _ := json.Marshal(approval)
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/api/v1/%s/graph-runs/%s/approvals", namespace, approval.GraphRunID), bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("CreateApproval: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateApproval %d: %s", resp.StatusCode, string(eb))
	}
	if err := json.NewDecoder(resp.Body).Decode(approval); err != nil {
		return fmt.Errorf("CreateApproval decode: %w", err)
	}
	return nil
}

// ListPendingApprovals returns all pending human approvals.
func (c *FlowgentClient) ListPendingApprovals(ctx context.Context, namespace string) ([]entities.ApprovalInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/approvals", nil)
	if err != nil {
		return nil, fmt.Errorf("ListPendingApprovals: %w", err)
	}
	var items []entities.ApprovalInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListPendingApprovals: %w", err)
	}
	return items, nil
}

// ─── Notifier Channels ───────────────────────────────────────────

// ListChannels returns notification channels for a namespace.
func (c *FlowgentClient) ListChannels(ctx context.Context, namespace string) ([]entities.NotifyChannelInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/notifications/channels", nil)
	if err != nil {
		return nil, fmt.Errorf("ListChannels: %w", err)
	}
	var page entities.Page[entities.NotifyChannelInfo]
	if err := readJSON(resp, &page); err != nil {
		return nil, fmt.Errorf("ListChannels: %w", err)
	}
	return dereferenceItems(page.Items), nil
}

// ListRuntimeChannels returns encrypted channel envelopes to the notifier.
func (c *FlowgentClient) ListRuntimeChannels(ctx context.Context, namespace string) ([]entities.NotifyChannelInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/notifications/runtime/channels", nil)
	if err != nil {
		return nil, fmt.Errorf("ListRuntimeChannels: %w", err)
	}
	var page entities.Page[entities.NotifyChannelInfo]
	if err := readJSON(resp, &page); err != nil {
		return nil, fmt.Errorf("ListRuntimeChannels: %w", err)
	}
	return dereferenceItems(page.Items), nil
}

// CreateChannel creates a notification channel.
func (c *FlowgentClient) CreateChannel(ctx context.Context, namespace string, ch *entities.NotifyChannelInfo) (*entities.NotifyChannelInfo, error) {
	b, _ := json.Marshal(ch)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/notifications/channels", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("CreateChannel: %w", err)
	}
	var created entities.NotifyChannelInfo
	if err := readJSON(resp, &created); err != nil {
		return nil, fmt.Errorf("CreateChannel: %w", err)
	}
	return &created, nil
}

// GetChannel returns a single notification channel.
func (c *FlowgentClient) GetChannel(ctx context.Context, namespace, id string) (*entities.NotifyChannelInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/notifications/channels/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("GetChannel: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetChannel %d: %s", resp.StatusCode, string(b))
	}
	var ch entities.NotifyChannelInfo
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		return nil, fmt.Errorf("GetChannel: %w", err)
	}
	return &ch, nil
}

// UpdateChannel updates a notification channel.
func (c *FlowgentClient) UpdateChannel(ctx context.Context, namespace, id string, ch *entities.NotifyChannelInfo) (*entities.NotifyChannelInfo, error) {
	b, _ := json.Marshal(ch)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/notifications/channels/"+id, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("UpdateChannel: %w", err)
	}
	var updated entities.NotifyChannelInfo
	if err := readJSON(resp, &updated); err != nil {
		return nil, fmt.Errorf("UpdateChannel: %w", err)
	}
	return &updated, nil
}

// DeleteChannel deletes a notification channel.
func (c *FlowgentClient) DeleteChannel(ctx context.Context, namespace, id string) error {
	resp, err := c.do(ctx, "DELETE", "/api/v1/"+namespace+"/notifications/channels/"+id, nil)
	if err != nil {
		return fmt.Errorf("DeleteChannel: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DeleteChannel %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ─── LLM Providers ───────────────────────────────────────────────

// ListLLMProviders returns DB-backed LLM provider definitions.
func (c *FlowgentClient) ListLLMProviders(ctx context.Context, namespace string) ([]*entities.LlmProviderInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/llm/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("ListLLMProviders: %w", err)
	}
	var items []*entities.LlmProviderInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListLLMProviders: %w", err)
	}
	for _, item := range items {
		if item != nil {
			item.Normalize()
		}
	}
	return items, nil
}

// ─── MCP Servers ──────────────────────────────────────────────────

// ListMCPs returns DB-backed MCP server definitions.
func (c *FlowgentClient) ListMCPs(ctx context.Context, namespace string) ([]*entities.McpInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/mcp", nil)
	if err != nil {
		return nil, fmt.Errorf("ListMCPs: %w", err)
	}
	var items []*entities.McpInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListMCPs: %w", err)
	}
	for _, item := range items {
		if item != nil {
			item.Normalize()
		}
	}
	return items, nil
}

// ─── Knowledge ─────────────────────────────────────────────────────

// SearchKnowledge performs a semantic/keyword search over the knowledge base.
func (c *FlowgentClient) SearchKnowledge(ctx context.Context, namespace string, query string, topK int, tags []string) ([]*entities.KnowledgeEntry, error) {
	body, _ := json.Marshal(map[string]any{
		"query": query,
		"top_k": topK,
		"tags":  tags,
	})
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/knowledge/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("SearchKnowledge: %w", err)
	}
	var items []*entities.KnowledgeEntry
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("SearchKnowledge: %w", err)
	}
	return items, nil
}

// CreateKnowledgeCandidate freezes a summary candidate and requests the
// human approval required before it can become retrievable knowledge or an
// active instruction.
func (c *FlowgentClient) CreateKnowledgeCandidate(ctx context.Context, namespace string, candidate *entities.KnowledgeCandidate) (*entities.ApprovalInfo, error) {
	b, _ := json.Marshal(candidate)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/knowledge/candidates", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("CreateKnowledgeCandidate: %w", err)
	}
	var result struct {
		Candidate entities.KnowledgeCandidate `json:"candidate"`
		Approval  entities.ApprovalInfo       `json:"approval"`
	}
	if err := readJSON(resp, &result); err != nil {
		return nil, fmt.Errorf("CreateKnowledgeCandidate: %w", err)
	}
	*candidate = result.Candidate
	return &result.Approval, nil
}

// ─── Watch (long-poll) ───────────────────────────────────────────

// WatchAgentGraphs calls GET /api/v1/{namespace}/agent-graphs/watch?since=N (long-poll).
func (c *FlowgentClient) WatchAgentGraphs(ctx context.Context, namespace string, since int64) ([]entities.AgentGraph, int64, error) {
	raw := fmt.Sprintf("/api/v1/%s/agent-graphs/watch?since=%d", namespace, since)
	resp, err := c.do(ctx, "GET", raw, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("WatchAgentGraphs: %w", err)
	}
	defer resp.Body.Close()
	var wrapper entities.AgentGraphWatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, 0, fmt.Errorf("decode watch response: %w", err)
	}
	return wrapper.AgentGraphs, wrapper.Cursor, nil
}

// ─── Client-backed state adapters ─────────────────────────────────

// GraphRunStateClient adapts FlowgentClient for JobMaster run/noderun persistence.
// Implements the narrow interface expected by the jobmanager engine package.
type GraphRunStateClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *GraphRunStateClient) UpdateGraphRun(ctx context.Context, run *entities.GraphRun) error {
	return a.Client.UpdateRunLifecycle(ctx, a.Namespace, run.ID, entities.GraphRunLifecycleUpdate{
		Status:     run.Status,
		Error:      run.Error,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	})
}

func (a *GraphRunStateClient) SaveNodeRun(ctx context.Context, noderun *entities.NodeRun) error {
	if noderun == nil {
		return fmt.Errorf("save noderun: noderun is required")
	}
	noderun.Normalize()
	return a.Client.UpdateNodeRun(ctx, a.Namespace, noderun.GraphRunID, noderun.ID, noderun)
}

// NodeRunStateClient adapts FlowgentClient for TM/SlotWorker noderun persistence.
type NodeRunStateClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *NodeRunStateClient) SaveNodeRun(ctx context.Context, noderun *entities.NodeRun) error {
	if noderun == nil {
		return fmt.Errorf("save noderun: noderun is required")
	}
	noderun.Normalize()
	return a.Client.UpdateNodeRun(ctx, a.Namespace, noderun.GraphRunID, noderun.ID, noderun)
}

// HumanApprovalClient adapts FlowgentClient for HumanExecutor approval creation.
type HumanApprovalClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *HumanApprovalClient) CreateApproval(ctx context.Context, approval *entities.ApprovalInfo) error {
	return a.Client.CreateApproval(ctx, a.Namespace, approval)
}

// NotifierClient is the client-side adapter for the notifier server's API calls.
type NotifierClient struct {
	Client    *FlowgentClient
	Namespace string
	routes    map[string]*model.SubscriptionRoute
	routesMu  sync.Mutex
}

func NewNotifierClient(c *FlowgentClient, namespace string) *NotifierClient {
	return &NotifierClient{
		Client:    c,
		Namespace: namespace,
		routes:    make(map[string]*model.SubscriptionRoute),
	}
}

func (a *NotifierClient) ListPendingApprovals(ctx context.Context) ([]entities.ApprovalInfo, error) {
	return a.Client.ListPendingApprovals(ctx, a.Namespace)
}

func (a *NotifierClient) ListChannels(ctx context.Context, namespaceID string) ([]entities.NotifyChannelInfo, error) {
	if namespaceID == "" {
		namespaceID = a.Namespace
	}
	return a.Client.ListRuntimeChannels(ctx, namespaceID)
}

func dereferenceItems[T any](items []*T) []T {
	result := make([]T, 0, len(items))
	for _, item := range items {
		if item != nil {
			result = append(result, *item)
		}
	}
	return result
}

func (a *NotifierClient) SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	a.routes[route.ID] = route
	return nil
}

func (a *NotifierClient) GetRoutesByAgentGraph(ctx context.Context, agentGraphID string) ([]model.SubscriptionRoute, error) {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	var result []model.SubscriptionRoute
	for _, r := range a.routes {
		if r.AgentGraphName == agentGraphID {
			result = append(result, *r)
		}
	}
	return result, nil
}

func (a *NotifierClient) DeleteRoute(ctx context.Context, id string) error {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	delete(a.routes, id)
	return nil
}

func (a *NotifierClient) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	var deleted int64
	for id, route := range a.routes {
		if route.PodID == podID && time.Since(route.CreatedAt) > maxAge {
			delete(a.routes, id)
			deleted++
		}
	}
	return deleted, nil
}

// LlmProviderClient adapts FlowgentClient for LLM provider loading.
type LlmProviderClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *LlmProviderClient) ListProviders(ctx context.Context) ([]*entities.LlmProviderInfo, error) {
	return a.Client.ListLLMProviders(ctx, a.Namespace)
}

// McpProviderClient adapts FlowgentClient for MCP server definition loading.
type McpProviderClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *McpProviderClient) ListMCPs(ctx context.Context) ([]*entities.McpInfo, error) {
	return a.Client.ListMCPs(ctx, a.Namespace)
}

// InitializeSubGraph returns the committed graph, pinning the referenced child
// definition on first use. It never replaces an existing runtime graph.
func (c *FlowgentClient) InitializeSubGraph(ctx context.Context, namespace, runID, instance string) (*entities.SubGraph, error) {
	return c.subgraphRequest(ctx, namespace, runID, "subgraphs/initialize", map[string]string{"instance_id": instance})
}
func (c *FlowgentClient) ApplyGraphPatch(ctx context.Context, namespace string, patch entities.GraphPatch) (*entities.SubGraph, error) {
	return c.subgraphRequest(ctx, namespace, patch.GraphRunID, "graph-patches", patch)
}
func (c *FlowgentClient) subgraphRequest(ctx context.Context, namespace, runID, path string, body any) (*entities.SubGraph, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/%s/graph-runs/%s/%s", url.PathEscape(namespace), url.PathEscape(runID), path), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var state entities.SubGraph
	if err = readJSON(resp, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
func (a *GraphRunStateClient) InitializeSubGraph(ctx context.Context, runID, instance string) (*entities.SubGraph, error) {
	return a.Client.InitializeSubGraph(ctx, a.Namespace, runID, instance)
}
func (a *GraphRunStateClient) ApplyGraphPatch(ctx context.Context, p entities.GraphPatch) (*entities.SubGraph, error) {
	return a.Client.ApplyGraphPatch(ctx, a.Namespace, p)
}
func (a *GraphRunStateClient) ListNodeRuns(ctx context.Context, runID string) ([]*entities.NodeRun, error) {
	state, err := a.Client.GetGraphRecovery(ctx, a.Namespace, runID)
	if err != nil {
		return nil, err
	}
	nodes := make([]*entities.NodeRun, len(state.NodeRuns))
	for i := range state.NodeRuns {
		nodes[i] = &state.NodeRuns[i]
	}
	return nodes, nil
}

func (a *NodeRunStateClient) ClaimNodeRun(ctx context.Context, plan *entities.ExecutionPlan, owner string) (bool, error) {
	data, err := json.Marshal(map[string]any{"node_instance_id": plan.NodeInstanceID, "dispatch_epoch": plan.DispatchEpoch, "owner": owner})
	if err != nil {
		return false, err
	}
	path := fmt.Sprintf("/api/v1/%s/graph-runs/%s/node-runs/%s/claim", url.PathEscape(a.Namespace), url.PathEscape(plan.GraphRunID), url.PathEscape(plan.NodeRunID))
	resp, err := a.Client.do(ctx, http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var result struct {
		Claimed bool `json:"claimed"`
	}
	err = readJSON(resp, &result)
	return result.Claimed, err
}

func (c *FlowgentClient) DispatchPlan(ctx context.Context, namespace string, plan *entities.ExecutionPlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/v1/%s/graph-runs/%s/node-runs/%s/dispatch", url.PathEscape(namespace), url.PathEscape(plan.GraphRunID), url.PathEscape(plan.NodeRunID))
	resp, err := c.do(ctx, http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dispatch %d: %s", resp.StatusCode, body)
	}
	return nil
}
func (c *FlowgentClient) GetGraphSnapshot(ctx context.Context, namespace, runID string) (*entities.AgentGraph, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/%s/graph-runs/%s/snapshot", url.PathEscape(namespace), url.PathEscape(runID)), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var spec entities.AgentGraph
	if err = readJSON(resp, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (c *FlowgentClient) CreateGraphCheckpoint(ctx context.Context, namespace, graphRunID string) (*entities.GraphCheckpoint, error) {
	response, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/%s/graph-runs/%s/checkpoints", url.PathEscape(namespace), url.PathEscape(graphRunID)), nil)
	if err != nil {
		return nil, err
	}
	var checkpoint entities.GraphCheckpoint
	if err = readJSON(response, &checkpoint); err != nil {
		return nil, err
	}
	return &checkpoint, nil
}
func (c *FlowgentClient) CommitNodeCheckpoint(ctx context.Context, namespace, graphRunID, nodeRunID string, epoch int64, checkpoint *entities.NodeCheckpoint) error {
	data, err := json.Marshal(map[string]any{"dispatch_epoch": epoch, "checkpoint": checkpoint})
	if err != nil {
		return err
	}
	response, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/%s/graph-runs/%s/node-runs/%s/checkpoint", url.PathEscape(namespace), url.PathEscape(graphRunID), url.PathEscape(nodeRunID)), bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("checkpoint %d: %s", response.StatusCode, body)
	}
	return nil
}
func (a *GraphRunStateClient) Checkpoint(ctx context.Context, graphRunID string) error {
	_, err := a.Client.CreateGraphCheckpoint(ctx, a.Namespace, graphRunID)
	return err
}

func (c *FlowgentClient) GetGraphRecovery(ctx context.Context, namespace, graphRunID string) (*entities.GraphRecovery, error) {
	response, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/%s/graph-runs/%s/recovery", url.PathEscape(namespace), url.PathEscape(graphRunID)), nil)
	if err != nil {
		return nil, err
	}
	var state entities.GraphRecovery
	if err = readJSON(response, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *FlowgentClient) GetRunAgent(ctx context.Context, namespace, graphRunID, name string) (*entities.AgentInfo, error) {
	if graphRunID == "" {
		return c.GetAgent(ctx, namespace, name)
	}
	response, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/%s/graph-runs/%s/agents/%s", url.PathEscape(namespace), url.PathEscape(graphRunID), url.PathEscape(name)), nil)
	if err != nil {
		return nil, err
	}
	var agent entities.AgentInfo
	if err = readJSON(response, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

func (c *FlowgentClient) DispatchSandbox(ctx context.Context, trigger *model.SandboxTrigger) error {
	return c.sandboxAction(ctx, trigger.Namespace, trigger.GraphRunID, trigger.NodeRunID, "sandbox-dispatch", trigger)
}
func (c *FlowgentClient) CommitSandboxResult(ctx context.Context, namespace, graphRunID, nodeRunID string, result *entities.NodeResult) error {
	return c.sandboxAction(ctx, namespace, graphRunID, nodeRunID, "sandbox-result", result)
}
func (c *FlowgentClient) sandboxAction(ctx context.Context, namespace, graphRunID, nodeRunID, action string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	response, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/%s/graph-runs/%s/node-runs/%s/%s", url.PathEscape(namespace), url.PathEscape(graphRunID), url.PathEscape(nodeRunID), action), bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("sandbox %s %d: %s", action, response.StatusCode, body)
	}
	return nil
}
