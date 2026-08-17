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
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// SandboxExecutor dispatches scripts to sandbox runner pods via MQTT + shared
// workspace volume. The workspace is a persistent volume mounted to both TM and
// sandbox pods, organized as:
//
//	{workspace}/{namespace_id}/{flow_id}/{run_id}/{task_plan_id}/
//	  ├── script.{py,sh,js}
//	  ├── input.json
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
	queue                 messager.IMessager
	policy                *model.SandboxPolicy
	workspace             string
	runtimeConfigResolver RuntimeConfigResolver
}

// RuntimeConfigResolver provides the effective namespace→Flow configuration.
// Resolving per attempt keeps shared resource-pool workers stateless and
// prevents one Flow's environment or secrets from leaking into another Flow.
type RuntimeConfigResolver interface {
	ResolveFlowRuntimeConfig(ctx context.Context, namespace, flowID string) (*entities.ResolvedRuntimeConfig, error)
}

func NewSandboxExecutor(q messager.IMessager, policy *model.SandboxPolicy, workspace string) *SandboxExecutor {
	return &SandboxExecutor{queue: q, policy: policy, workspace: workspace}
}

func (e *SandboxExecutor) SetRuntimeConfigResolver(resolver RuntimeConfigResolver) {
	e.runtimeConfigResolver = resolver
}

func (e *SandboxExecutor) TaskType() entities.TaskType { return entities.TaskSandbox }

func (e *SandboxExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	if e.policy != nil && plan.NodeSpec != nil && plan.NodeSpec.NetworkPolicy == nil {
		plan.NodeSpec.NetworkPolicy = &e.policy.Network
	}

	spanID := newSpanID()
	scriptPath := e.buildPath(plan)
	plan.NodeSpec.ScriptPath = scriptPath

	if err := e.writeScript(plan); err != nil {
		return nil, fmt.Errorf("sandbox write script: %w", err)
	}

	if plan.Input != nil {
		if err := e.writeInput(plan); err != nil {
			return nil, fmt.Errorf("sandbox write input: %w", err)
		}
	}
	if plan.NodeSpec.Workspace != "" {
		if err := e.snapshotOriginals(plan); err != nil {
			return nil, fmt.Errorf("sandbox snapshot: %w", err)
		}
	}

	trigger := &model.SandboxTrigger{
		Namespace:      plan.Namespace,
		ResourcePoolID: plan.ResourcePoolID,
		FlowID:         plan.AgentFlowDefinitionID,
		RunID:          plan.AgentFlowRunID,
		PlanID:         plan.PlanID,
		ScriptPath:     scriptPath,
		Runtime:        plan.NodeSpec.Runtime,
		Timeout:        plan.NodeSpec.Timeout,
		Resources:      plan.NodeSpec.Resources,
		NetworkPolicy:  plan.NodeSpec.NetworkPolicy,
		Workspace:      plan.NodeSpec.Workspace,
		SpanID:         spanID,
	}
	resolvedEnv, err := e.resolveEnvironment(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("sandbox resolve runtime configuration: %w", err)
	}
	trigger.Env = resolvedEnv
	payload, err := json.Marshal(trigger)
	if err != nil {
		return nil, fmt.Errorf("sandbox marshal trigger: %w", err)
	}

	triggerTopic := messager.SandboxTriggerTopic(plan.Namespace, plan.ResourcePoolID, plan.AgentFlowDefinitionID, plan.AgentFlowRunID)
	resultTopic := messager.SandboxResultTopic(plan.Namespace, plan.AgentFlowDefinitionID, plan.AgentFlowRunID)

	resultCh := make(chan *entities.TaskResult, 1)

	// Subscribe BEFORE publishing to avoid race (result arrives before subscriber is ready).
	if err := e.queue.Subscribe(ctx, resultTopic, func(topic string, payload []byte) {
		var result entities.TaskResult
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
	poll := time.NewTicker(time.Second)
	defer poll.Stop()

	for {
		select {
		case result := <-resultCh:
			return result, nil
		case <-poll.C:
			if result, err := e.readResultFile(scriptPath); err == nil {
				return result, nil
			}
		case <-waitCtx.Done():
			if result, err := e.readResultFile(scriptPath); err == nil {
				return result, nil
			}
			return &entities.TaskResult{Error: "sandbox execution timeout"}, nil
		}
	}
}

func (e *SandboxExecutor) resolveEnvironment(ctx context.Context, plan *entities.ExecutionPlan) (map[string]string, error) {
	env := config.LoadCredentials("/var/flowgent", plan.Namespace, plan.AgentFlowDefinitionID, nil)
	if env == nil {
		env = make(map[string]string)
	}
	if e.runtimeConfigResolver == nil {
		return env, nil
	}
	resolved, err := e.runtimeConfigResolver.ResolveFlowRuntimeConfig(ctx, plan.Namespace, plan.AgentFlowDefinitionID)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return env, nil
	}
	for key, value := range resolved.Environment {
		env[key] = value
	}
	// A secret intentionally wins over an environment entry with the same key.
	for key, value := range resolved.Secrets {
		env[key] = value
	}
	return env, nil
}

func (e *SandboxExecutor) buildPath(plan *entities.ExecutionPlan) string {
	return filepath.Join(
		e.workspace,
		sanitize(plan.Namespace),
		sanitize(plan.AgentFlowDefinitionID),
		sanitize(plan.AgentFlowRunID),
		sanitize(plan.PlanID),
	)
}

func (e *SandboxExecutor) writeInput(plan *entities.ExecutionPlan) error {
	dir := plan.NodeSpec.ScriptPath
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan.Input, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "input.json"), data, 0600)
}

func (e *SandboxExecutor) writeScript(plan *entities.ExecutionPlan) error {
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

func (e *SandboxExecutor) snapshotOriginals(plan *entities.ExecutionPlan) error {
	origDir := filepath.Join(plan.NodeSpec.ScriptPath, "original")
	if err := os.MkdirAll(origDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("cp", "-a", plan.NodeSpec.Workspace, origDir+"/workspace")
	return cmd.Run()
}

func (e *SandboxExecutor) readResultFile(scriptPath string) (*entities.TaskResult, error) {
	data, err := os.ReadFile(filepath.Join(scriptPath, "result.json"))
	if err != nil {
		return nil, err
	}
	var result entities.TaskResult
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
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	return s
}
