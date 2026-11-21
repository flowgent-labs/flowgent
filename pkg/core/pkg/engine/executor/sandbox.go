package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	sandbox "github.com/flowgent-labs/flowgent/sandbox/pkg"
)

// SandboxExecutor dispatches scripts to sandbox runner pods via MQTT + shared
// workspace volume. The workspace is a persistent volume mounted to both TM and
// sandbox pods, organized as:
//
//	{workspace}/{namespace_id}/{agent_graph_id}/{graph_run_id}/{task_plan_id}/
//	  ├── script.{py,sh,js}
//	  ├── input.json
//	  ├── result.json
//	  ├── status
//	  └── original/   (pre-modification snapshot for undo)
//
// In distributed mode (sandbox as independent pods), triggers are routed to the
// run's runtime cluster:
//
//	flowgent/v1/{namespaceId}/clusters/{clusterId}/agent-graphs/{graphId}/graph-runs/{runId}/sandbox/trigger
//
// Results come back on:
//
//	flowgent/v1/{namespaceId}/agent-graphs/{graphId}/graph-runs/{runId}/sandbox/result
type SandboxExecutor struct {
	queue                 messager.IMessager
	policy                *model.SandboxPolicy
	workspace             string
	runtimeConfigResolver RuntimeConfigResolver
	dispatcher            interface {
		DispatchSandbox(context.Context, *model.SandboxTrigger) error
	}
}

// RuntimeConfigResolver provides the effective namespace→AgentGraph configuration.
// Resolving per attempt keeps long-lived workers stateless and
// prevents one AgentGraph's environment or secrets from leaking into another AgentGraph.
type RuntimeConfigResolver interface {
	ResolveGraphRuntimeConfig(ctx context.Context, namespace, graphID string) (*entities.ResolvedRuntimeConfig, error)
}

func NewSandboxExecutor(q messager.IMessager, policy *model.SandboxPolicy, workspace string) *SandboxExecutor {
	return &SandboxExecutor{queue: q, policy: policy, workspace: workspace}
}

func (e *SandboxExecutor) SetRuntimeConfigResolver(resolver RuntimeConfigResolver) {
	e.runtimeConfigResolver = resolver
	if dispatcher, ok := resolver.(interface {
		DispatchSandbox(context.Context, *model.SandboxTrigger) error
	}); ok {
		e.dispatcher = dispatcher
	}
}

func (e *SandboxExecutor) NodeKind() entities.NodeKind { return entities.SandboxNode }

func (e *SandboxExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	if plan == nil || plan.Node == nil {
		return nil, fmt.Errorf("sandbox execution plan and node spec are required")
	}
	if e.queue == nil {
		return nil, fmt.Errorf("sandbox message queue is not configured")
	}
	networkPolicy := plan.Node.NetworkPolicy
	if networkPolicy == nil && e.policy != nil {
		networkPolicy = &e.policy.Network
	}

	spanID := newSpanID()
	scriptPath := e.buildPath(plan)
	if plan.Checkpoint != nil && plan.Checkpoint.WorkspaceSnapshot != nil && plan.Checkpoint.ScriptPath != "" {
		root, _ := filepath.Abs(e.workspace)
		candidate, _ := filepath.Abs(plan.Checkpoint.ScriptPath)
		relative, err := filepath.Rel(root, candidate)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("checkpoint script path escapes workspace")
		}
		scriptPath = candidate
	}
	plan.WorkspacePath = scriptPath

	if plan.Checkpoint == nil || plan.Checkpoint.WorkspaceSnapshot == nil {
		if err := e.writeScript(plan); err != nil {
			return nil, fmt.Errorf("sandbox write script: %w", err)
		}

		if plan.Input != nil {
			if err := e.writeInput(plan); err != nil {
				return nil, fmt.Errorf("sandbox write input: %w", err)
			}
		}

	}
	trigger := &model.SandboxTrigger{
		Attempt:   plan.RetryCount + 1,
		NodeRunID: plan.NodeRunID, NodeInstanceID: plan.NodeInstanceID, DispatchEpoch: plan.DispatchEpoch,
		Namespace:        plan.Namespace,
		RuntimeClusterID: plan.RuntimeClusterID,
		AgentGraphID:     plan.AgentGraphID,
		AgentGraphName:   plan.AgentGraphName,
		GraphRunID:       plan.GraphRunID,
		PlanID:           plan.PlanID,
		ScriptPath:       scriptPath,
		Runtime:          plan.Node.Runtime,
		Timeout:          plan.Node.Timeout,
		Resources:        plan.Node.Resources,
		NetworkPolicy:    networkPolicy,
		Workspace:        plan.Node.Workspace,
		SpanID:           spanID,
	}
	if plan.Checkpoint != nil {
		trigger.Checkpoint, _ = json.Marshal(plan.Checkpoint)
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

	triggerTopic := messager.SandboxTriggerTopic(plan.Namespace, plan.RuntimeClusterID, plan.AgentGraphID, plan.GraphRunID)
	resultTopic := messager.SandboxResultTopic(plan.Namespace, plan.AgentGraphID, plan.GraphRunID)

	resultCh := make(chan *entities.NodeResult, 1)

	// Subscribe BEFORE publishing to avoid race (result arrives before subscriber is ready).
	if err := e.queue.Subscribe(ctx, resultTopic, func(topic string, payload []byte) {
		var result entities.NodeResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return
		}
		if !e.validResult(ctx, plan, &result) {
			return
		}
		select {
		case resultCh <- &result:
		default:
		}
	}); err != nil {
		return nil, fmt.Errorf("sandbox subscribe: %w", err)
	}

	if plan.DispatchEpoch > 0 && e.dispatcher != nil {
		// Only explicit non-secret node environment is sent durably. Runtime secret
		// resolution happens in Sandbox after the dispatch is accepted.
		trigger.Env = nil
		if err := e.dispatcher.DispatchSandbox(ctx, trigger); err != nil {
			return nil, fmt.Errorf("persist sandbox dispatch: %w", err)
		}
	} else {
		if err := e.queue.Publish(ctx, triggerTopic, &messager.InterMessage{
			ID: plan.PlanID, Payload: payload,
		}); err != nil {
			return nil, fmt.Errorf("sandbox publish: %w", err)
		}

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
			if result, err := e.readResultFile(ctx, plan, scriptPath); err == nil {
				return result, nil
			}
		case <-waitCtx.Done():
			if result, err := e.readResultFile(ctx, plan, scriptPath); err == nil {
				return result, nil
			}
			return &entities.NodeResult{Error: "sandbox execution timeout"}, nil
		}
	}
}

func (e *SandboxExecutor) resolveEnvironment(ctx context.Context, plan *entities.ExecutionPlan) (map[string]string, error) {
	env := config.LoadCredentials("/var/flowgent", plan.Namespace, plan.AgentGraphName)
	if env == nil {
		env = make(map[string]string)
	}
	if e.runtimeConfigResolver == nil {
		return env, nil
	}
	resolved, err := e.runtimeConfigResolver.ResolveGraphRuntimeConfig(ctx, plan.Namespace, plan.AgentGraphName)
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
		sanitize(plan.AgentGraphName),
		sanitize(plan.GraphRunID),
		sanitize(plan.NodeRunID),
	)
}

func (e *SandboxExecutor) writeInput(plan *entities.ExecutionPlan) error {
	dir := plan.WorkspacePath
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
	dir := plan.WorkspacePath
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ext := "sh"
	switch plan.Node.Runtime {
	case "python3":
		ext = "py"
	case "node":
		ext = "js"
	}
	if err := os.WriteFile(filepath.Join(dir, "script."+ext), []byte(plan.Node.Script), 0700); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(dir, "status"), []byte("PENDING"), 0644)
	return nil
}

func (e *SandboxExecutor) readResultFile(ctx context.Context, plan *entities.ExecutionPlan, scriptPath string) (*entities.NodeResult, error) {
	data, err := os.ReadFile(filepath.Join(scriptPath, "result.json"))
	if err != nil {
		return nil, err
	}
	var result entities.NodeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if !e.validResult(ctx, plan, &result) {
		return nil, fmt.Errorf("sandbox result identity or snapshot verification failed")
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

func (e *SandboxExecutor) validResult(ctx context.Context, plan *entities.ExecutionPlan, result *entities.NodeResult) bool {
	if plan.DispatchEpoch == 0 {
		return true
	}
	if result.NodeRunID != plan.NodeRunID || result.DispatchEpoch != plan.DispatchEpoch {
		return false
	}
	if result.Error != "" {
		return true
	}
	if result.WorkspaceSnapshot == nil {
		return false
	}
	return (sandbox.WorkspaceStore{Root: filepath.Join(e.workspace, ".snapshots")}).Verify(ctx, result.WorkspaceSnapshot) == nil
}
