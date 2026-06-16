package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
		baseURL = "http://flowgent-apiserver:9999"
	}
	log.Printf("[api-client] using apiserver at %s", baseURL)
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

// ListAgents returns all agent definitions for a tenant.
func (c *FlowgentClient) ListAgents(ctx context.Context, tenant string) ([]*entities.AgentInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/agents", nil)
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
func (c *FlowgentClient) GetAgent(ctx context.Context, tenant, name string) (*entities.AgentInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/agents/"+name, nil)
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

func (c *FlowgentClient) ListFlows(ctx context.Context, tenant string) ([]entities.AgentFlowVersionInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/agentflows", nil)
	if err != nil {
		return nil, fmt.Errorf("ListFlows: %w", err)
	}
	var items []entities.AgentFlowVersionInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListFlows: %w", err)
	}
	return items, nil
}

func (c *FlowgentClient) GetFlow(ctx context.Context, tenant, flowID string) (*entities.AgentFlowInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/agentflows/"+flowID, nil)
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
	var spec entities.AgentFlowInfo
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (c *FlowgentClient) CreateFlow(ctx context.Context, tenant string, spec *entities.AgentFlowInfo) error {
	b, _ := json.Marshal(spec)
	resp, err := c.do(ctx, "POST", "/api/v1/"+tenant+"/agentflows", bytes.NewReader(b))
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

func (c *FlowgentClient) UpdateFlow(ctx context.Context, tenant, flowID string, spec *entities.AgentFlowInfo) error {
	b, _ := json.Marshal(spec)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+tenant+"/agentflows/"+flowID, bytes.NewReader(b))
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

func (c *FlowgentClient) DeleteFlow(ctx context.Context, tenant, flowID string) error {
	resp, err := c.do(ctx, "DELETE", "/api/v1/"+tenant+"/agentflows/"+flowID, nil)
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

// CreateRun creates a run directly with explicit tenant, namespace, and trigger info.
func (c *FlowgentClient) CreateRun(ctx context.Context, tenant string, run *entities.FlowRunInfo) (*entities.FlowRunInfo, error) {
	b, _ := json.Marshal(run)
	resp, err := c.do(ctx, "POST", "/api/v1/"+tenant+"/runs", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("CreateRun: %w", err)
	}
	var created entities.FlowRunInfo
	if err := readJSON(resp, &created); err != nil {
		return nil, fmt.Errorf("CreateRun: %w", err)
	}
	return &created, nil
}

// TriggerRun creates a PENDING run via the trigger endpoint.
func (c *FlowgentClient) TriggerRun(ctx context.Context, tenant, agentFlowID string, vars map[string]any, trigger entities.TriggerInfo) (*entities.FlowRunInfo, error) {
	payload := map[string]any{"agentflow_id": agentFlowID, "vars": vars, "trigger": trigger}
	b, _ := json.Marshal(payload)
	resp, err := c.do(ctx, "POST", "/api/v1/"+tenant+"/agentflows/trigger", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("TriggerRun: %w", err)
	}
	var out entities.FlowRunInfo
	if err := readJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("TriggerRun: %w", err)
	}
	log.Printf("[api-client] TriggerRun: %s/%s", tenant, agentFlowID)
	return &out, nil
}

// GetRun returns a run by tenant and ID.
func (c *FlowgentClient) GetRun(ctx context.Context, tenant, runID string) (*entities.FlowRunInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/runs/"+runID, nil)
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

// ListRuns returns runs with optional filters. status, namespace, and flowID may be empty.
func (c *FlowgentClient) ListRuns(ctx context.Context, tenant, status, namespace, flowID string, page, size int) (*entities.Page[entities.FlowRunInfo], error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if namespace != "" {
		q.Set("namespace", namespace)
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
	raw := "/api/v1/" + tenant + "/runs?" + q.Encode()
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

// UpdateRunStatus updates the status of a run.
func (c *FlowgentClient) UpdateRunStatus(ctx context.Context, tenant, runID, status string, errStr string) error {
	b, _ := json.Marshal(map[string]string{"status": status, "error": errStr})
	resp, err := c.do(ctx, "PUT", "/api/v1/"+tenant+"/runs/"+runID, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("UpdateRunStatus: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UpdateRunStatus %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// CancelRun cancels a run.
func (c *FlowgentClient) CancelRun(ctx context.Context, tenant, runID string) error {
	resp, err := c.do(ctx, "POST", "/api/v1/"+tenant+"/runs/"+runID+"/cancel", nil)
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
func (c *FlowgentClient) CreateTaskRun(ctx context.Context, tenant, runID string, task *entities.TaskRunInfo) error {
	b, _ := json.Marshal(task)
	resp, err := c.do(ctx, "POST", "/api/v1/"+tenant+"/runs/"+runID+"/tasks", bytes.NewReader(b))
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
func (c *FlowgentClient) UpdateTaskRun(ctx context.Context, tenant, runID, taskID string, task *entities.TaskRunInfo) error {
	b, _ := json.Marshal(task)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+tenant+"/runs/"+runID+"/tasks/"+taskID, bytes.NewReader(b))
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
func (c *FlowgentClient) ListTaskRuns(ctx context.Context, tenant, runID string) ([]*entities.TaskRunInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/runs/"+runID+"/tasks", nil)
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
func (c *FlowgentClient) GetTaskRun(ctx context.Context, tenant, runID, taskID string) (*entities.TaskRunInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/runs/"+runID+"/tasks/"+taskID, nil)
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
func (c *FlowgentClient) SavePlan(ctx context.Context, tenant, runID string, plan *entities.ExecutionPlan) error {
	// execution plans flow through the tasks endpoint
	task := &entities.TaskRunInfo{
		BaseEntity:     entities.BaseEntity{ID: plan.TaskID},
		AgentFlowRunID: plan.AgentFlowRunID,
		NodeID:         plan.NodeID,
		Status:         plan.State,
		Input:          plan.Input,
		ExecID:         plan.PlanID,
	}
	return c.CreateTaskRun(ctx, tenant, runID, task)
}

// ─── Human Approvals ─────────────────────────────────────────────

// CreateApproval creates a pending human approval.
func (c *FlowgentClient) CreateApproval(ctx context.Context, approval *entities.ApprovalInfo) error {
	b, _ := json.Marshal(approval)
	resp, err := c.do(ctx, "POST", "/api/v1/human/approvals", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("CreateApproval: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		eb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateApproval %d: %s", resp.StatusCode, string(eb))
	}
	return nil
}

// ListPendingApprovals returns all pending human approvals.
func (c *FlowgentClient) ListPendingApprovals(ctx context.Context) ([]entities.ApprovalInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/human/approvals?status=PENDING", nil)
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

// ListChannels returns notification channels for a tenant.
func (c *FlowgentClient) ListChannels(ctx context.Context, tenant string) ([]entities.NotifyChannelInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/notifications/channels", nil)
	if err != nil {
		return nil, fmt.Errorf("ListChannels: %w", err)
	}
	var items []entities.NotifyChannelInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListChannels: %w", err)
	}
	return items, nil
}

// CreateChannel creates a notification channel.
func (c *FlowgentClient) CreateChannel(ctx context.Context, tenant string, ch *entities.NotifyChannelInfo) (*entities.NotifyChannelInfo, error) {
	b, _ := json.Marshal(ch)
	resp, err := c.do(ctx, "POST", "/api/v1/"+tenant+"/notifications/channels", bytes.NewReader(b))
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
func (c *FlowgentClient) GetChannel(ctx context.Context, tenant, id string) (*entities.NotifyChannelInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/notifications/channels/"+id, nil)
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
func (c *FlowgentClient) UpdateChannel(ctx context.Context, tenant, id string, ch *entities.NotifyChannelInfo) (*entities.NotifyChannelInfo, error) {
	b, _ := json.Marshal(ch)
	resp, err := c.do(ctx, "PUT", "/api/v1/"+tenant+"/notifications/channels/"+id, bytes.NewReader(b))
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
func (c *FlowgentClient) DeleteChannel(ctx context.Context, tenant, id string) error {
	resp, err := c.do(ctx, "DELETE", "/api/v1/"+tenant+"/notifications/channels/"+id, nil)
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
func (c *FlowgentClient) ListLLMProviders(ctx context.Context, tenant string) ([]*entities.LlmProviderInfo, error) {
	resp, err := c.do(ctx, "GET", "/api/v1/"+tenant+"/llm/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("ListLLMProviders: %w", err)
	}
	var items []*entities.LlmProviderInfo
	if err := readJSON(resp, &items); err != nil {
		return nil, fmt.Errorf("ListLLMProviders: %w", err)
	}
	return items, nil
}

// ─── Watch (long-poll) ───────────────────────────────────────────

// WatchFlows calls GET /api/v1/{tenant}/agentflows/watch?since=N (long-poll).
func (c *FlowgentClient) WatchFlows(ctx context.Context, tenant string, since int64) ([]entities.AgentFlowVersionInfo, int64, error) {
	raw := fmt.Sprintf("/api/v1/%s/agentflows/watch?since=%d", tenant, since)
	resp, err := c.do(ctx, "GET", raw, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("WatchFlows: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var versions []entities.AgentFlowVersionInfo
	if err := json.Unmarshal(body, &versions); err == nil {
		return versions, since, nil
	}
	var wrapper struct {
		Flows   []entities.AgentFlowVersionInfo `json:"flows"`
		Version int64                    `json:"version"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, 0, fmt.Errorf("decode watch response: %w", err)
	}
	if wrapper.Version > 0 {
		since = wrapper.Version
	}
	return wrapper.Flows, since, nil
}

// ─── Client-backed state adapters ─────────────────────────────────

// RunStateClient adapts FlowgentClient for JobMaster run/task persistence.
// Implements the narrow interface expected by the jobmanager engine package.
type RunStateClient struct {
	Client *FlowgentClient
	Tenant string
}

func (a *RunStateClient) UpdateRun(ctx context.Context, run *entities.FlowRunInfo) error {
	return a.Client.UpdateRunStatus(ctx, a.Tenant, run.ID, string(run.Status), run.Error)
}

func (a *RunStateClient) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error {
	return a.Client.UpdateTaskRun(ctx, a.Tenant, task.AgentFlowRunID, task.ID, task)
}

// TaskStateClient adapts FlowgentClient for TM/SlotWorker task persistence.
type TaskStateClient struct {
	Client *FlowgentClient
	Tenant string
}

func (a *TaskStateClient) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error {
	return a.Client.UpdateTaskRun(ctx, a.Tenant, task.AgentFlowRunID, task.ID, task)
}

// HumanApprovalClient adapts FlowgentClient for HumanExecutor approval creation.
type HumanApprovalClient struct {
	Client *FlowgentClient
}

func (a *HumanApprovalClient) CreateApproval(ctx context.Context, approval *entities.ApprovalInfo) error {
	return a.Client.CreateApproval(ctx, approval)
}

// NotifierClient is the client-side adapter for the notifier server's API calls.
type NotifierClient struct {
	Client  *FlowgentClient
	Tenant  string
	routes  map[string]*model.SubscriptionRoute
	routesMu sync.Mutex
}

func NewNotifierClient(c *FlowgentClient, tenant string) *NotifierClient {
	return &NotifierClient{
		Client: c,
		Tenant: tenant,
		routes: make(map[string]*model.SubscriptionRoute),
	}
}

func (a *NotifierClient) ListPendingApprovals(ctx context.Context) ([]entities.ApprovalInfo, error) {
	return a.Client.ListPendingApprovals(ctx)
}

func (a *NotifierClient) ListChannels(ctx context.Context, tenantID string) ([]entities.NotifyChannelInfo, error) {
	return a.Client.ListChannels(ctx, tenantID)
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
	Client *FlowgentClient
	Tenant string
}

func (a *LlmProviderClient) ListProviders(ctx context.Context) ([]*entities.LlmProviderInfo, error) {
	return a.Client.ListLLMProviders(ctx, a.Tenant)
}
