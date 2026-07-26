package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
)

func StartTaskManager(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
	}
	return startTaskManager(cfgPath)
}

func StopTaskManager(pidFile string) error { return utils.StopByPID(pidFile) }

func RestartTaskManager(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		utils.WritePID(pidFile)
	}
	return startTaskManager(cfgPath)
}

func startTaskManager(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logMode, logLevel := svcCfg.Logging.Mode, svcCfg.Logging.Level
	logger := utils.NewLogger(logMode, logLevel)

	defaultTMID := "application-tm-" + utils.Hostname()
	if flowID := svcCfg.Runtime.AgentFlowID; flowID != "" {
		defaultTMID = "application-" + svcCfg.Runtime.Tenant.DefaultTenant + "-" + flowID + "-tm-" + utils.Hostname()
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
	tenant := svcCfg.Runtime.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	tm, err := taskmanager.NewTaskManager(&taskmanager.TaskManagerConfig{
		ID: tmID, SlotCount: slotCount, Messager: q,
		State:         &client.TaskStateClient{Client: apiClient, Tenant: tenant},
		ApprovalInfo:  &client.HumanApprovalClient{Client: apiClient},
		APIServerURL:  svcCfg.Runtime.APIServerURL,
		Tenant:        tenant,
		Logger:        logger,
		SandboxMessager:             q,
		SandboxPolicy:               svcCfg.Sandbox.Policy,
		SandboxWorkspace:            svcCfg.Sandbox.Workspace,
		HttpClient:                  client.NewHttpClient(svcCfg, q),
	})
	if err != nil {
		return fmt.Errorf("create taskmanager: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tm.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	slog.Info("TaskManager started", "tmID", tmID, "slots", slotCount)
	utils.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}
