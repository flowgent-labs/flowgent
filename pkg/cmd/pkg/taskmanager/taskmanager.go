// Package taskmanager provides the TaskManager daemon entry point.
// The TaskManager is a persistent worker that consumes ExecutionPlans from MQTT
// and executes them via a slot pool.
package taskmanager

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
)

// Start launches the TaskManager daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
	}
	return startTaskManager(cfgPath)
}

// Stop stops the TaskManager daemon.
func Stop(pidFile string) error {
	return utils.StopByPID(pidFile)
}

// Restart restarts the TaskManager daemon.
func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		utils.WritePID(pidFile)
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
	defaultTMID := mode + "-tm-" + utils.Hostname()
	if flowID := svcCfg.Runtime.AgentFlowID; flowID != "" && mode == "application" {
		defaultTMID = mode + "-" + svcCfg.Tenant.DefaultTenant + "-" + flowID + "-tm-" + utils.Hostname()
	}
	tmID := svcCfg.Runtime.TMID
	if tmID == "" {
		tmID = defaultTMID
	}
	slotCount := svcCfg.Runtime.TMSlots
	if slotCount == 0 {
		slotCount = 4
	}

	q := messager.NewQueueFromConfig(svcCfg, tmID)
	defer q.Close()

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	tenant := svcCfg.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	var agentPtrs []*config.AgentDef
	if svcCfg != nil {
		if agents, err := config.LoadAgents(svcCfg, cfgPath); err == nil {
			agentPtrs = make([]*config.AgentDef, len(agents))
			for i := range agents {
				agentPtrs[i] = &agents[i]
			}
		}
	}

	// ── MCP Clients (DB-backed, loaded at runtime) ────
	_ = mcp.NewMcpManager() // MCPs now DB-backed, loaded at runtime
	mcpMap := make(map[string]engine.MCPClient)


	tm, err := taskmanager.NewTaskManager(&taskmanager.TaskManagerConfig{
		ID: tmID, SlotCount: slotCount, Queue: q,
		State:         &client.TaskStateClient{Client: apiClient, Tenant: tenant},
		HumanApproval: &client.HumanApprovalClient{Client: apiClient},
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
	utils.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}
