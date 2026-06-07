package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	model "github.com/flowgent-labs/flowgent/model/pkg"
	store "github.com/flowgent-labs/flowgent/store/pkg"
)

// FlowgentClient provides HTTP access to the API Server.
// Used by non-DB components (controller, JM, TM, sandbox, notifier, a2a)
// instead of direct PG connections.
type FlowgentClient struct {
	BaseURL string
}

func NewFlowgentClient() *FlowgentClient {
	baseURL := os.Getenv("FLOWGENT_APISERVER_URL")
	if baseURL == "" {
		baseURL = "http://flowgent-apiserver:9999"
	}
	log.Printf("[api-client] using apiserver at %s", baseURL)
	return &FlowgentClient{BaseURL: baseURL}
}

// ─── AgentFlow CRUD ───────────────────────────────────────────

// ListFlows returns all agentflow definitions.
func (c *FlowgentClient) ListFlows(ctx context.Context, tenant string) ([]model.AgentFlowVersion, error) {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows", c.BaseURL, tenant)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiserver ListFlows: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("apiserver returned %d: %s", resp.StatusCode, string(body))
	}
	var versions []model.AgentFlowVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("decode flows: %w", err)
	}
	return versions, nil
}

// GetFlow returns a single agentflow spec by ID.
func (c *FlowgentClient) GetFlow(ctx context.Context, tenant, flowID string) (*model.AgentFlowSpec, error) {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows/%s", c.BaseURL, tenant, flowID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiserver GetFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apiserver returned %d", resp.StatusCode)
	}
	var spec model.AgentFlowSpec
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// CreateFlow creates a new agentflow definition.
func (c *FlowgentClient) CreateFlow(ctx context.Context, tenant string, spec *model.AgentFlowSpec) error {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows", c.BaseURL, tenant)
	b, _ := json.Marshal(spec)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("apiserver CreateFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver CreateFlow %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// UpdateFlow updates an existing agentflow definition.
func (c *FlowgentClient) UpdateFlow(ctx context.Context, tenant, flowID string, spec *model.AgentFlowSpec) error {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows/%s", c.BaseURL, tenant, flowID)
	b, _ := json.Marshal(spec)
	req, _ := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("apiserver UpdateFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver UpdateFlow %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// DeleteFlow deletes an agentflow definition.
func (c *FlowgentClient) DeleteFlow(ctx context.Context, tenant, flowID string) error {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows/%s", c.BaseURL, tenant, flowID)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("apiserver DeleteFlow: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver DeleteFlow %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// ─── FlowRun Control ──────────────────────────────────────────

// TriggerRun creates a PENDING run for the given agentflow.
func (c *FlowgentClient) TriggerRun(ctx context.Context, agentFlowID string, vars map[string]any, trigger model.TriggerInfo) error {
	url := fmt.Sprintf("%s/api/v1/default/agentflows/trigger", c.BaseURL)
	payload := map[string]interface{}{
		"agentflow_id": agentFlowID,
		"vars":         vars,
		"trigger":      trigger,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("apiserver TriggerRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver TriggerRun %d: %s", resp.StatusCode, string(errBody))
	}
	log.Printf("[api-client] TriggerRun: %s → PENDING", agentFlowID)
	return nil
}

// GetRun returns a run by ID.
func (c *FlowgentClient) GetRun(ctx context.Context, runID string) (*model.AgentFlowRun, error) {
	url := fmt.Sprintf("%s/api/v1/default/runs/%s", c.BaseURL, runID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiserver GetRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apiserver returned %d", resp.StatusCode)
	}
	var run model.AgentFlowRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns recent runs, optionally filtered by flow ID.
func (c *FlowgentClient) ListRuns(ctx context.Context, flowID string) ([]model.AgentFlowRun, error) {
	url := fmt.Sprintf("%s/api/v1/default/runs?flow_id=%s", c.BaseURL, flowID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiserver ListRuns: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("apiserver returned %d: %s", resp.StatusCode, string(body))
	}
	var runs []model.AgentFlowRun
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		return nil, err
	}
	return runs, nil
}

// CancelRun cancels a running flow.
func (c *FlowgentClient) CancelRun(ctx context.Context, runID string) error {
	url := fmt.Sprintf("%s/api/v1/default/runs/%s/cancel", c.BaseURL, runID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("apiserver CancelRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver CancelRun %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// ─── Task Update ──────────────────────────────────────────────

// UpdateTask updates a task run status via the apiserver.
func (c *FlowgentClient) UpdateTask(ctx context.Context, taskID string, status string, output map[string]any, errStr string) error {
	url := fmt.Sprintf("%s/api/v1/default/runs/_/tasks/%s", c.BaseURL, taskID)
	payload := map[string]interface{}{
		"id": taskID, "status": status, "output": output, "error": errStr,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("apiserver UpdateTask: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver UpdateTask %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// ─── Watch (long-poll for Controller) ─────────────────────────

// WatchFlows calls GET /api/v1/{tenant}/agentflows/watch?since=N (long-poll).
func (c *FlowgentClient) WatchFlows(ctx context.Context, tenant string, since int64) ([]model.AgentFlowVersion, int64, error) {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows/watch?since=%d", c.BaseURL, tenant, since)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("apiserver WatchFlows: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var versions []model.AgentFlowVersion
	if err := json.Unmarshal(body, &versions); err == nil {
		return versions, since, nil
	}
	var wrapper struct {
		Flows   []model.AgentFlowVersion `json:"flows"`
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

// ─── APIStoreWrapper ──────────────────────────────────────────

// APIStoreWrapper delegates UpdateTaskRun and SavePlan to apiserver API.
type APIStoreWrapper struct {
	api *FlowgentClient
}

func NewAPIStoreWrapper(inner store.IStore, api *FlowgentClient) store.IStore {
	_ = inner
	return &APIStoreWrapper{api: api}
}

func (w *APIStoreWrapper) DB() any  { return nil }
func (w *APIStoreWrapper) Close() error { return nil }

func (w *APIStoreWrapper) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	return w.api.UpdateTask(ctx, task.ID, string(task.Status), task.Output, task.Error)
}

func (w *APIStoreWrapper) SavePlan(ctx context.Context, plan *model.ExecutionPlan) error {
	return nil
}
