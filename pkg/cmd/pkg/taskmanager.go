package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/messager/pkg"
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

	if svcCfg.Mgmt.OTEL.Enabled && svcCfg.Mgmt.OTEL.Endpoint != "" {
		otelCfg := &tracing.OTELConfig{
			Enabled: svcCfg.Mgmt.OTEL.Enabled, Endpoint: svcCfg.Mgmt.OTEL.Endpoint,
			Protocol: svcCfg.Mgmt.OTEL.Protocol, Timeout: svcCfg.Mgmt.OTEL.Timeout,
			SampleRate: svcCfg.Mgmt.OTEL.SampleRate,
		}
		if provider, err := tracing.NewProvider(context.Background(), "flowgent-taskmanager", "1.0", otelCfg, nil); err != nil {
			slog.Warn("OTEL tracer provider init failed, tracing disabled", "error", err)
		} else {
			defer provider.Shutdown(context.Background())
			slog.Info("OTEL tracing enabled", "endpoint", svcCfg.Mgmt.OTEL.Endpoint)
		}
	}

	poolID := svcCfg.Runtime.ResourcePoolID
	if poolID == "" {
		return fmt.Errorf("runtime.resource_pool_id is required")
	}
	defaultTMID := "pool-" + svcCfg.Runtime.Namespace.DefaultNamespace + "-" + poolID + "-tm-" + utils.Hostname()
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
	namespace := svcCfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}

	tm, err := taskmanager.NewTaskManager(&taskmanager.TaskManagerConfig{
		ID: tmID, SlotCount: slotCount, Messager: q,
		State:                    &client.TaskStateClient{Client: apiClient, Namespace: namespace},
		ApprovalInfo:             &client.HumanApprovalClient{Client: apiClient},
		APIServerURL:             svcCfg.Runtime.APIServerURL,
		Namespace:                namespace,
		ResourcePoolID:           poolID,
		Logger:                   logger,
		SandboxMessager:          q,
		SandboxPolicy:            svcCfg.Sandbox.Policy,
		SandboxWorkspace:         svcCfg.Sandbox.Workspace,
		SandboxDeploymentEnabled: svcCfg.Sandbox.Deployment.Enabled,
		HttpClient:               client.NewHttpClient(svcCfg, q),
	})
	if err != nil {
		return fmt.Errorf("create taskmanager: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tm.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	slog.Info("TaskManager started", "tmID", tmID, "resource_pool", poolID, "slots", slotCount)
	utils.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}
