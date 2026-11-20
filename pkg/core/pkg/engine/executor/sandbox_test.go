package executor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type runtimeConfigResolverStub struct {
	value *entities.ResolvedRuntimeConfig
	err   error
}

func TestSandboxExecutorRejectsMissingPlanAndQueue(t *testing.T) {
	executor := NewSandboxExecutor(nil, nil, t.TempDir())

	if _, err := executor.Execute(context.Background(), nil, nil); err == nil ||
		!strings.Contains(err.Error(), "plan and node spec") {
		t.Fatalf("Execute(nil) error = %v, want plan validation error", err)
	}

	plan := &entities.ExecutionPlan{Node: &entities.GraphNode{}}
	if _, err := executor.Execute(context.Background(), plan, nil); err == nil ||
		!strings.Contains(err.Error(), "message queue") {
		t.Fatalf("Execute() error = %v, want missing queue error", err)
	}
}

func (s runtimeConfigResolverStub) ResolveGraphRuntimeConfig(context.Context, string, string) (*entities.ResolvedRuntimeConfig, error) {
	return s.value, s.err
}

func TestSandboxExecutorBuildPathUsesRuntimeHierarchy(t *testing.T) {
	e := NewSandboxExecutor(nil, nil, "/var/flowgent")
	plan := &entities.ExecutionPlan{
		Namespace:      "default",
		AgentGraphName: "security-autonomy-fixer",
		GraphRunID:     "run-123",
		PlanID:         "plan-run-123-git-clone", NodeRunID: "node-attempt-1",
	}

	got := e.buildPath(plan)
	want := filepath.Join(
		"/var/flowgent",
		"default",
		"security-autonomy-fixer",
		"run-123",
		"node-attempt-1",
	)
	if got != want {
		t.Fatalf("buildPath() = %q, want %q", got, want)
	}
}

func TestSandboxExecutorBuildPathSanitizesSegments(t *testing.T) {
	e := NewSandboxExecutor(nil, nil, "/var/flowgent")
	plan := &entities.ExecutionPlan{
		Namespace:      "team/../a",
		AgentGraphName: "graph\\b",
		GraphRunID:     "run/1",
		PlanID:         "plan/1", NodeRunID: "node/1",
	}

	got := e.buildPath(plan)
	want := filepath.Join("/var/flowgent", "team___a", "graph_b", "run_1", "node_1")
	if got != want {
		t.Fatalf("buildPath() = %q, want %q", got, want)
	}
}

func TestSandboxExecutorResolvesAgentGraphConfigurationPerPlan(t *testing.T) {
	e := NewSandboxExecutor(nil, nil, "/var/flowgent")
	e.SetRuntimeConfigResolver(runtimeConfigResolverStub{value: &entities.ResolvedRuntimeConfig{
		Environment: map[string]string{"SHARED": "environment", "FLOW_ONLY": "one"},
		Secrets:     map[string]string{"SHARED": "secret", "TOKEN": "write-only"},
	}})
	env, err := e.resolveEnvironment(context.Background(), &entities.ExecutionPlan{
		Namespace: "team-a", AgentGraphName: "graph-a",
	})
	if err != nil {
		t.Fatalf("resolveEnvironment() error = %v", err)
	}
	if env["FLOW_ONLY"] != "one" || env["TOKEN"] != "write-only" {
		t.Fatalf("resolved AgentGraph configuration missing: %#v", env)
	}
	if env["SHARED"] != "secret" {
		t.Fatalf("secret must override environment entry, got %q", env["SHARED"])
	}
}
