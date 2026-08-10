package executor

import (
	"path/filepath"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestSandboxExecutorBuildPathUsesRuntimeHierarchy(t *testing.T) {
	e := NewSandboxExecutor(nil, nil, "/var/flowgent")
	plan := &entities.ExecutionPlan{
		Namespace:             "default",
		AgentFlowDefinitionID: "security-autonomy-fixer",
		AgentFlowRunID:        "run-123",
		PlanID:                "plan-run-123-git-clone",
	}

	got := e.buildPath(plan)
	want := filepath.Join(
		"/var/flowgent",
		"default",
		"security-autonomy-fixer",
		"run-123",
		"plan-run-123-git-clone",
	)
	if got != want {
		t.Fatalf("buildPath() = %q, want %q", got, want)
	}
}

func TestSandboxExecutorBuildPathSanitizesSegments(t *testing.T) {
	e := NewSandboxExecutor(nil, nil, "/var/flowgent")
	plan := &entities.ExecutionPlan{
		Namespace:             "team/../a",
		AgentFlowDefinitionID: "flow\\b",
		AgentFlowRunID:        "run/1",
		PlanID:                "plan/1",
	}

	got := e.buildPath(plan)
	want := filepath.Join("/var/flowgent", "team___a", "flow_b", "run_1", "plan_1")
	if got != want {
		t.Fatalf("buildPath() = %q, want %q", got, want)
	}
}
