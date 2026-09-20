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

// ─── AgentFlow CRUD ──────────────────────────────────────────────

func (c *FlowgentClient) ListFlows(ctx context.Context, namespace string) ([]entities.FlowInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/flows", nil)
	if err != nil {
		return nil, fmt.Errorf("ListFlows: %w", err)
	}
	var items []entities.FlowInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListFlows: %w", err)
	}
	return items, nil
}

func (c *FlowgentClient) GetFlow(ctx context.Context, namespace, flowID string) (*entities.FlowInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/flows/"+flowID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetFlow %d: %s", resp.StatusCode, string(b))
	}
	var spec entities.FlowInfo
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// ResolveFlowRuntimeConfig returns the effective namespace→Flow runtime
// configuration for the controller. This endpoint is intentionally unavailable
// to human/API-key roles because it contains plaintext secret values.
func (c *FlowgentClient) ResolveFlowRuntimeConfig(ctx context.Context, namespace, flowID string) (*entities.ResolvedRuntimeConfig, error) {
	path := "/api/v1/" + url.PathEscape(namespace) + "/flows/" + url.PathEscape(flowID) + "/runtime-config/resolved"
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("ResolveFlowRuntimeConfig: %w", err)
	}
	var resolved entities.ResolvedRuntimeConfig
	if err := readJSON(resp, &resolved); err != nil {
		return nil, fmt.Errorf("ResolveFlowRuntimeConfig: %w", err)
	}
	if resolved.Environment == nil {
		resolved.Environment = map[string]string{}
	}
	if resolved.Secrets == nil {
		resolved.Secrets = map[string]string{}
	}
	return &resolved, nil
}

func (c *FlowgentClient) CreateFlow(ctx context.Context, namespace string, spec *entities.FlowInfo) error {
	b, _ := json.Marshal(spec)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/flows", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("CreateFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateFlow %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

func (c *FlowgentClient) UpdateFlow(ctx context.Context, namespace, flowID string, spec *entities.FlowInfo) error {
	b, _ := json.Marshal(spec)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/flows/"+flowID, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("UpdateFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UpdateFlow %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

func (c *FlowgentClient) DeleteFlow(ctx context.Context, namespace, flowID string) error {
	resp, err := c.do(ctx, "DELETE", "/api/v1/"+namespace+"/flows/"+flowID, nil)
	if err != nil {
		return fmt.Errorf("DeleteFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DeleteFlow %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ─── FlowRun ─────────────────────────────────────────────────────

// CreateRun creates a run directly with explicit namespace, namespace, and trigger info.
func (c *FlowgentClient) CreateRun(ctx context.Context, namespace string, run *entities.FlowRunInfo) (*entities.FlowRunInfo, error) {
	b, _ := json.Marshal(run)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/runs", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("CreateRun: %w", err)
	}
	var created entities.FlowRunInfo
	if err := readJSON(resp, &created); err != nil {
		return nil, fmt.Errorf("CreateRun: %w", err)
	}
	return &created, nil
}

// TriggerRunResult is the narrow response contract of POST /flows/trigger.
// The trigger endpoint intentionally returns an acknowledgement rather than a
// full FlowRunInfo; callers that need the run body can subsequently call
// GetRun with RunID.
type TriggerRunResult struct {
	RunID       string             `json:"run_id"`
	Status      entities.RunStatus `json:"status"`
	Namespace   string             `json:"namespace"`
	AgentFlowID string             `json:"agentflow_id"`
}

// TriggerRun creates a PENDING run via the trigger endpoint.
func (c *FlowgentClient) TriggerRun(ctx context.Context, namespace, agentFlowID string, vars map[string]any, trigger entities.TriggerInfo) (*TriggerRunResult, error) {
	payload := map[string]any{"agentflow_id": agentFlowID, "vars": vars, "trigger": trigger}
	b, _ := json.Marshal(payload)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/flows/trigger", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("TriggerRun: %w", err)
	}
	var out TriggerRunResult
	if err := readJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("TriggerRun: %w", err)
	}
	slog.Debug("api client TriggerRun", "namespace", namespace, "agentFlowID", agentFlowID)
	return &out, nil
}

// GetRun returns a run by namespace and ID.
func (c *FlowgentClient) GetRun(ctx context.Context, namespace, runID string) (*entities.FlowRunInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/runs/"+runID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetRun %d: %s", resp.StatusCode, string(b))
	}
	var run entities.FlowRunInfo
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns runs with optional filters. status, runtimeMode, k8sNamespace, and flowID may be empty.
func (c *FlowgentClient) ListRuns(ctx context.Context, namespace, status, runtimeMode, k8sNamespace, flowID string, page, size int) (*entities.Page[entities.FlowRunInfo], error) {
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
	if flowID != "" {
		q.Set("agentflow_id", flowID)
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	raw := "/api/v1/" + namespace + "/runs?" + q.Encode()
	resp, err := c.do(ctx, "GET", raw, nil)
	if err != nil {
		return nil, fmt.Errorf("ListRuns: %w", err)
	}
	var pageResp entities.Page[entities.FlowRunInfo]
	if err := readJSON(resp, &pageResp); err != nil {
		return nil, fmt.Errorf("ListRuns: %w", err)
	}
	return &pageResp, nil
}

// UpdateRunLifecycle persists the lifecycle values produced by JobMaster.
func (c *FlowgentClient) UpdateRunLifecycle(ctx context.Context, namespace, runID string, update entities.RunLifecycleUpdate) error {
	b, _ := json.Marshal(update)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/runs/"+runID, bytes.NewReader(b))
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

// CancelRun cancels a run.
func (c *FlowgentClient) CancelRun(ctx context.Context, namespace, runID string) error {
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/runs/"+runID+"/cancel", nil)
	if err != nil {
		return fmt.Errorf("CancelRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CancelRun %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ─── TaskRuns ────────────────────────────────────────────────────

// CreateTaskRun persists a new task run.
func (c *FlowgentClient) CreateTaskRun(ctx context.Context, namespace, runID string, task *entities.TaskRunInfo) error {
	b, _ := json.Marshal(task)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/runs/"+runID+"/tasks", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("CreateTaskRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateTaskRun %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// UpdateTaskRun updates an existing task run (status, output, error).
func (c *FlowgentClient) UpdateTaskRun(ctx context.Context, namespace, runID, taskID string, task *entities.TaskRunInfo) error {
	b, _ := json.Marshal(task)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+namespace+"/runs/"+runID+"/tasks/"+taskID, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("UpdateTaskRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UpdateTaskRun %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ListTaskRuns returns all tasks for a run.
func (c *FlowgentClient) ListTaskRuns(ctx context.Context, namespace, runID string) ([]*entities.TaskRunInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/runs/"+runID+"/tasks", nil)
	if err != nil {
		return nil, fmt.Errorf("ListTaskRuns: %w", err)
	}
	var items []*entities.TaskRunInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListTaskRuns: %w", err)
	}
	return items, nil
}

// GetTaskRun returns a single task run.
func (c *FlowgentClient) GetTaskRun(ctx context.Context, namespace, runID, taskID string) (*entities.TaskRunInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+namespace+"/runs/"+runID+"/tasks/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetTaskRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetTaskRun %d: %s", resp.StatusCode, string(b))
	}
	var item entities.TaskRunInfo
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

// SavePlan creates/updates the execution plan state via apiserver.
func (c *FlowgentClient) SavePlan(ctx context.Context, namespace, runID string, plan *entities.ExecutionPlan) error {
	// execution plans flow through the tasks endpoint
	task := &entities.TaskRunInfo{
		BaseEntity:     entities.BaseEntity{ID: plan.TaskID},
		AgentFlowRunID: plan.AgentFlowRunID,
		NodeID:         plan.NodeID,
		Status:         plan.State,
		Input:          plan.Input,
		ExecID:         plan.PlanID,
	}
	return c.CreateTaskRun(ctx, namespace, runID, task)
}

// ─── Human Approvals ─────────────────────────────────────────────

// CreateApproval creates a pending human approval.
func (c *FlowgentClient) CreateApproval(ctx context.Context, namespace string, approval *entities.ApprovalInfo) error {
	b, _ := json.Marshal(approval)
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/api/v1/%s/runs/%s/approvals", namespace, approval.AgentFlowRunID), bytes.NewReader(b))
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

// CreateKnowledge creates a knowledge entry via the API.
func (c *FlowgentClient) CreateKnowledge(ctx context.Context, namespace string, entry *entities.KnowledgeEntry) (*entities.KnowledgeEntry, error) {
	b, _ := json.Marshal(entry)
	resp, err := c.do(ctx, "POST", "/api/v1/"+namespace+"/knowledge", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("CreateKnowledge: %w", err)
	}
	var created entities.KnowledgeEntry
	if err := readJSON(resp, &created); err != nil {
		return nil, fmt.Errorf("CreateKnowledge: %w", err)
	}
	return &created, nil
}

// ─── Watch (long-poll) ───────────────────────────────────────────

// WatchFlows calls GET /api/v1/{namespace}/flows/watch?since=N (long-poll).
func (c *FlowgentClient) WatchFlows(ctx context.Context, namespace string, since int64) ([]entities.FlowInfo, int64, error) {
	raw := fmt.Sprintf("/api/v1/%s/flows/watch?since=%d", namespace, since)
	resp, err := c.do(ctx, "GET", raw, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("WatchFlows: %w", err)
	}
	defer resp.Body.Close()
	var wrapper entities.FlowWatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, 0, fmt.Errorf("decode watch response: %w", err)
	}
	return wrapper.Flows, wrapper.Version, nil
}

// ─── Client-backed state adapters ─────────────────────────────────

// RunStateClient adapts FlowgentClient for JobMaster run/task persistence.
// Implements the narrow interface expected by the jobmanager engine package.
type RunStateClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *RunStateClient) UpdateRun(ctx context.Context, run *entities.FlowRunInfo) error {
	return a.Client.UpdateRunLifecycle(ctx, a.Namespace, run.ID, entities.RunLifecycleUpdate{
		Status:     run.Status,
		Error:      run.Error,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	})
}

func (a *RunStateClient) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error {
	return a.Client.UpdateTaskRun(ctx, a.Namespace, task.AgentFlowRunID, task.ID, task)
}

// TaskStateClient adapts FlowgentClient for TM/SlotWorker task persistence.
type TaskStateClient struct {
	Client    *FlowgentClient
	Namespace string
}

func (a *TaskStateClient) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error {
	return a.Client.UpdateTaskRun(ctx, a.Namespace, task.AgentFlowRunID, task.ID, task)
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

func (a *NotifierClient) GetRoutesByFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error) {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	var result []model.SubscriptionRoute
	for _, r := range a.routes {
		if r.AgentFlowID == agentFlowID {
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
