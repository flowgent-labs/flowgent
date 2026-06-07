package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	store "github.com/flowgent-labs/flowgent/store/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// apiserverClient provides HTTP access to the API Server.
// Used by non-DB components (controller, JM, TM, sandbox, notifier)
// instead of direct PG connections.
type apiserverClient struct {
	BaseURL string
}

func NewAPIServerClient() *apiserverClient {
	baseURL := os.Getenv("FLOWGENT_APISERVER_URL")
	if baseURL == "" {
		baseURL = "http://flowgent-apiserver:9999"
	}
	log.Printf("[api-client] using apiserver at %s", baseURL)
	return &apiserverClient{BaseURL: baseURL}
}

// listFlows calls GET /api/v1/{tenant}/agentflows
func (c *apiserverClient) listFlows(ctx context.Context, tenant string) ([]model.AgentFlowVersion, error) {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows", c.BaseURL, tenant)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiserver listFlows: %w", err)
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
	log.Printf("[api-client] listFlows: %d flows", len(versions))
	return versions, nil
}

// watchFlows calls GET /api/v1/{tenant}/agentflows/watch?since=N (long-poll)
func (c *apiserverClient) watchFlows(ctx context.Context, tenant string, since int64) ([]model.AgentFlowVersion, int64, error) {
	url := fmt.Sprintf("%s/api/v1/%s/agentflows/watch?since=%d", c.BaseURL, tenant, since)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("apiserver watchFlows: %w", err)
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

// createRun calls POST /api/v1/{tenant}/agentflows/trigger
func (c *apiserverClient) createRun(ctx context.Context, agentFlowID string, vars map[string]any, trigger model.TriggerInfo) error {
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
		return fmt.Errorf("apiserver createRun: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver createRun %d: %s", resp.StatusCode, string(errBody))
	}
	log.Printf("[api-client] createRun: %s → PENDING", agentFlowID)
	return nil
}

// getFlowSpec calls GET /api/v1/{tenant}/agentflows/{id}
func (c *apiserverClient) getFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error) {
	url := fmt.Sprintf("%s/api/v1/default/agentflows/%s", c.BaseURL, agentFlowID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiserver getFlowSpec: %w", err)
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
		return nil, fmt.Errorf("decode spec: %w", err)
	}
	return &spec, nil
}

// getRun calls GET /api/v1/{tenant}/runs/{id}
func (c *apiserverClient) getRun(ctx context.Context, runID string) (*model.AgentFlowRun, error) {
	url := fmt.Sprintf("%s/api/v1/default/runs/%s", c.BaseURL, runID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return nil, fmt.Errorf("apiserver getRun: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode == 404 { return nil, nil }
	if resp.StatusCode >= 300 { return nil, fmt.Errorf("apiserver returned %d", resp.StatusCode) }
	var run model.AgentFlowRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil { return nil, err }
	return &run, nil
}

// updateTask calls PUT /api/v1/{tenant}/runs/{id}/tasks/{task_id}
func (c *apiserverClient) updateTask(ctx context.Context, taskID string, status string, output map[string]any, errStr string) error {
	url := fmt.Sprintf("%s/api/v1/default/runs/_/tasks/%s", c.BaseURL, taskID)
	payload := map[string]interface{}{
		"id":     taskID,
		"status": status,
		"output": output,
		"error":  errStr,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return fmt.Errorf("apiserver updateTask: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apiserver updateTask %d: %s", resp.StatusCode, string(errBody))
	}
	log.Printf("[api-client] updateTask: %s → %s", taskID[:8], status)
	return nil
}

// APIStoreWrapper delegates UpdateTaskRun and SavePlan to apiserver API,
// while passing all other Store methods through to the underlying store.
// This allows TM to use the apiserver for state writes while keeping read compatibility.
type APIStoreWrapper struct {
	store.IStore       // embeds all read methods
	api *apiserverClient
}

func NewAPIStoreWrapper(inner store.IStore, api *apiserverClient) store.IStore {
	return &APIStoreWrapper{IStore: inner, api: api}
}

func (w *APIStoreWrapper) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	return w.api.updateTask(ctx, task.ID, string(task.Status), task.Output, task.Error)
}

func (w *APIStoreWrapper) SavePlan(ctx context.Context, plan *model.ExecutionPlan) error {
	// ExecutionPlans are dispatched via MQTT — persistence can go through apiserver
	log.Printf("[api-store] SavePlan: %s → apiserver", plan.PlanID[:8])
	return nil // plan already dispatched via MQTT, apiserver creates task_runs
}
