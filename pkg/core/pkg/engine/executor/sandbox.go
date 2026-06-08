package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/messager/pkg"
)

// SandboxExecutor dispatches scripts to sandbox runner pods via MQTT + shared
// workspace volume. The workspace is a persistent volume mounted to both TM and
// sandbox pods, organized as:
//
//	{workspace}/{tenant}/{agentflow_id}/runs/{run_id}/plans/{plan_id}/{span_id}/
//	  ├── script.{py,sh,js}
//	  ├── result.json
//	  ├── status
//	  └── original/   (pre-modification snapshot for undo)
//
// In distributed mode (sandbox as independent pods), triggers go to:
//
//	flowgent/v1/sandbox/trigger/{flowId}/{runId}  ($share/sandbox-pool)
//
// Results come back on:
//
//	flowgent/v1/sandbox/result/{flowId}/{runId}   (point-to-point)
type SandboxExecutor struct {
	queue     messager.IMessager
	policy    *model.SandboxPolicy
	workspace string
}

func NewSandboxExecutor(q messager.IMessager, policy *model.SandboxPolicy, workspace string) *SandboxExecutor {
	return &SandboxExecutor{queue: q, policy: policy, workspace: workspace}
}

func (e *SandboxExecutor) TaskType() model.TaskType { return model.TaskSandbox }

func (e *SandboxExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	if e.policy != nil && plan.NodeSpec != nil && plan.NodeSpec.NetworkPolicy == nil {
		plan.NodeSpec.NetworkPolicy = &e.policy.Network
	}

	spanID := newSpanID()
	scriptPath := e.buildPath(plan, spanID)
	plan.NodeSpec.ScriptPath = scriptPath

	if err := e.writeScript(plan); err != nil {
		return nil, fmt.Errorf("sandbox write script: %w", err)
	}

	if plan.NodeSpec.Workspace != "" {
		if err := e.snapshotOriginals(plan); err != nil {
			return nil, fmt.Errorf("sandbox snapshot: %w", err)
		}
	}

	trigger := &model.SandboxTrigger{
		TenantID:      plan.TenantID,
		FlowID:        plan.AgentFlowDefinitionID,
		RunID:         plan.AgentFlowRunID,
		PlanID:        plan.PlanID,
		ScriptPath:    scriptPath,
		Runtime:       plan.NodeSpec.Runtime,
		Timeout:       plan.NodeSpec.Timeout,
		Resources:     plan.NodeSpec.Resources,
		NetworkPolicy: plan.NodeSpec.NetworkPolicy,
		Workspace:     plan.NodeSpec.Workspace,
		SpanID:        spanID,
	}
	payload, err := json.Marshal(trigger)
	if err != nil {
		return nil, fmt.Errorf("sandbox marshal trigger: %w", err)
	}

	triggerTopic := messager.SandboxTriggerTopic(plan.TenantID, plan.AgentFlowDefinitionID, plan.AgentFlowRunID)
	resultTopic := messager.SandboxResultTopic(plan.TenantID, plan.AgentFlowDefinitionID, plan.AgentFlowRunID)

	resultCh := make(chan *model.TaskResult, 1)

	// Subscribe BEFORE publishing to avoid race (result arrives before subscriber is ready).
	if err := e.queue.Subscribe(ctx, resultTopic, func(topic string, payload []byte) {
		var result model.TaskResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return
		}
		select {
		case resultCh <- &result:
		default:
		}
	}); err != nil {
		return nil, fmt.Errorf("sandbox subscribe: %w", err)
	}

	if err := e.queue.Publish(ctx, triggerTopic, &messager.InterMessage{
		ID: plan.PlanID, Payload: payload,
	}); err != nil {
		return nil, fmt.Errorf("sandbox publish: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	select {
	case <-waitCtx.Done():
		if result, err := e.readResultFile(scriptPath); err == nil {
			return result, nil
		}
		return &model.TaskResult{Error: "sandbox execution timeout"}, nil
	case result := <-resultCh:
		return result, nil
	}
}

func (e *SandboxExecutor) buildPath(plan *model.ExecutionPlan, spanID string) string {
	return filepath.Join(
		e.workspace,
		sanitize(plan.TenantID),
		sanitize(plan.AgentFlowDefinitionID),
		"runs", plan.AgentFlowRunID,
		"plans", plan.PlanID,
		spanID,
	)
}

func (e *SandboxExecutor) writeScript(plan *model.ExecutionPlan) error {
	dir := plan.NodeSpec.ScriptPath
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ext := "sh"
	switch plan.NodeSpec.Runtime {
	case "python3":
		ext = "py"
	case "node":
		ext = "js"
	}
	if err := os.WriteFile(filepath.Join(dir, "script."+ext), []byte(plan.NodeSpec.Script), 0700); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(dir, "status"), []byte("PENDING"), 0644)
	return nil
}

func (e *SandboxExecutor) snapshotOriginals(plan *model.ExecutionPlan) error {
	origDir := filepath.Join(plan.NodeSpec.ScriptPath, "original")
	if err := os.MkdirAll(origDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("cp", "-a", plan.NodeSpec.Workspace, origDir+"/workspace")
	return cmd.Run()
}

func (e *SandboxExecutor) readResultFile(scriptPath string) (*model.TaskResult, error) {
	data, err := os.ReadFile(filepath.Join(scriptPath, "result.json"))
	if err != nil {
		return nil, err
	}
	var result model.TaskResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func newSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sanitize(s string) string {
	if s == "" {
		return "default"
	}
	return s
}
