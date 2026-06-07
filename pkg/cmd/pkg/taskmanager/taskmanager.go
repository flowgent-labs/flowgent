// Package taskmanager provides the TaskManager daemon entry point.
// The TaskManager is a persistent worker that consumes ExecutionPlans from MQTT
// and executes them via a slot pool.
package taskmanager

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/flowgent-labs/flowgent/cmd/pkg/cmdutil"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// Start launches the TaskManager daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
	}
	return startTaskManager(cfgPath)
}

// Stop stops the TaskManager daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the TaskManager daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
	}
	return startTaskManager(cfgPath)
}

// startTaskManager is the actual TaskManager startup logic.
func startTaskManager(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logMode, logLevel := svcCfg.Logging.Mode, svcCfg.Logging.Level
	logger := utils.NewLogger(logMode, logLevel)

	// Build tmID with mode prefix for heartbeat topic differentiation.
	mode := "session"
	if svcCfg != nil && svcCfg.Deployment.Mode != "" {
		mode = svcCfg.Deployment.Mode
	}
	defaultTMID := mode + "-tm-" + cmdutil.Hostname()
	if flowID := cmdutil.EnvOr("FLOWGENT_AGENTFLOW_ID", ""); flowID != "" && mode == "application" {
		defaultTMID = mode + "-" + svcCfg.Tenant.DefaultTenant + "-" + flowID + "-tm-" + cmdutil.Hostname()
	}
	tmID := cmdutil.EnvOr("FLOWGENT_TM_ID", defaultTMID)
	slotCount := cmdutil.EnvIntOr("FLOWGENT_TM_SLOTS", 4)

	q := cmdutil.NewQueueFromConfig(svcCfg, tmID)
	defer q.Close()

	dbStore := store.NewStoreManager(svcCfg)

	var agentPtrs []*config.AgentDef
	if svcCfg != nil {
		if agents, err := config.LoadAgents(svcCfg, cfgPath); err == nil {
			agentPtrs = make([]*config.AgentDef, len(agents))
			for i := range agents {
				agentPtrs[i] = &agents[i]
			}
		}
	}

	// ── MCP Clients ────────────────────────────────────
	mcpFactory := mcp.NewMcpManager()
	for _, mcpDef := range svcCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpFactory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env)
		}
	}
	mcpMap := make(map[string]engine.MCPClient)
	for _, mcpDef := range svcCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpMap[mcpDef.Name] = &cmdutil.McpAdapter{Factory: mcpFactory, Name: mcpDef.Name}
		}
	}

	tm, err := taskmanager.NewTaskManager(&taskmanager.TaskManagerConfig{
		ID: tmID, SlotCount: slotCount, Queue: q, Store: dbStore,
		Agents: agentPtrs, MCPClients: mcpMap, Logger: logger,
		SandboxQueue:     q,
		SandboxPolicy:    svcCfg.Sandbox.Policy,
		SandboxWorkspace: svcCfg.Sandbox.Workspace,
	})
	if err != nil {
		return fmt.Errorf("create taskmanager: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tm.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	log.Printf("TaskManager %s started (slots=%d)", tmID, slotCount)
	cmdutil.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}
