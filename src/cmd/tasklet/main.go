package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/store"
	"github.com/flowgent-labs/flowgent/src/util"
)

// tasklet is the Kubernetes Job entry point for distributed node execution.
// It connects to the shared Postgres store via FLOWGENT_DATABASE_URL,
// reads the TaskSubmit from FLOWGENT_TASK_SUBMIT env, and executes the node.
//
// This binary is packaged as a container image and launched by
// KubernetesScheduler as a batch/v1 Job.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	submitJSON := os.Getenv("FLOWGENT_TASK_SUBMIT")
	if submitJSON == "" {
		slog.Error("FLOWGENT_TASK_SUBMIT env not set")
		os.Exit(1)
	}

	var submit engine.TaskSubmit
	if err := json.Unmarshal([]byte(submitJSON), &submit); err != nil {
		slog.Error("failed to parse FLOWGENT_TASK_SUBMIT", "error", err)
		os.Exit(1)
	}

	slog.Info("tasklet starting",
		"run_id", submit.RunID,
		"node_id", submit.NodeID,
		"agentflow_id", submit.AgentFlowID,
	)

	dbURL := os.Getenv("FLOWGENT_DATABASE_URL")
	if dbURL == "" {
		slog.Error("FLOWGENT_DATABASE_URL env not set (required for distributed mode)")
		os.Exit(1)
	}

	dbStore := store.NewPostgresStore(dbURL)
	logger := util.NewLogger("JSON", "DEBUG")

	// Minimal TaskManager: shared store only.
	// MCP/LLM clients would be injected by the K8s pod config in production.
	taskManager := engine.NewTaskManager(dbStore, nil, nil, nil, logger)

	taskRun, err := dbStore.GetTaskRun(ctx, submit.TaskID)
	if err != nil || taskRun == nil {
		slog.Error("task run not found", "task_id", submit.TaskID, "error", err)
		os.Exit(1)
	}

	run, err := dbStore.GetAgentFlowRun(ctx, submit.RunID)
	if err != nil || run == nil {
		slog.Error("agentflow run not found", "run_id", submit.RunID, "error", err)
		os.Exit(1)
	}

	scope := make(map[string]map[string]any)
	if run.Vars != nil {
		scope["vars"] = run.Vars
	}
	if submit.Input != nil {
		scope["input"] = submit.Input
	}

	if err := taskManager.ExecuteNode(ctx, taskRun, submit.Node, scope); err != nil {
		slog.Error("node execution failed", "node_id", submit.NodeID, "error", err)
		os.Exit(1)
	}

	slog.Info("tasklet completed successfully",
		"run_id", submit.RunID,
		"node_id", submit.NodeID,
		"status", taskRun.Status,
	)
}
