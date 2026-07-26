package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	ctrl "github.com/flowgent-labs/flowgent/controller/pkg"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
)

func StartController(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	logSuffix := ""
	if pidFile != "" {
		logSuffix = fmt.Sprintf(", pidfile=%s", pidFile)
	}
	slog.Info(fmt.Sprintf("Flowgent Controller starting (pid=%d%s)", os.Getpid(), logSuffix))
	return startController(cfgPath)
}

func StopController(pidFile string) error { return utils.StopByPID(pidFile) }

func RestartController(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startController(cfgPath)
}

func startController(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logMode, logLevel := "JSON", "DEBUG"
	if svcCfg != nil {
		logMode, logLevel = svcCfg.Logging.Mode, svcCfg.Logging.Level
	}
	logger := utils.NewLogger(logMode, logLevel)

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	tenant := svcCfg.Runtime.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	stateClient := &client.TaskStateClient{Client: apiClient, Tenant: tenant}
	humanClient := &client.HumanApprovalClient{Client: apiClient}

	rm, err := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:      engine.ProviderStandalone,
		PoolSize:      svcCfg.Orchestration.MaxConcurrentFlows,
		TaskState:     stateClient,
		ApprovalInfo: humanClient,
		Logger:        logger,
		APIServerURL:  svcCfg.Runtime.APIServerURL,
		Tenant:        tenant,
	})
	if err != nil {
		return fmt.Errorf("create resource manager: %w", err)
	}

	var disc discovery.IDiscoveryClient
	if k8sDisc, err := discovery.NewK8sDiscoveryClient(); err == nil {
		disc = k8sDisc
		logger.Info("Controller using K8s discovery client")
	} else {
		disc = discovery.NewStaticDiscoveryClient(svcCfg.Runtime.PodTotal, svcCfg.Runtime.PodIndex)
		logger.Info("Controller using static discovery client (env vars)")
	}

	flowCtrl := ctrl.NewFlowgentController(apiClient, tenant, rm, logger, svcCfg, cfgPath, disc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		slog.Info("Controller received shutdown signal")
		cancel()
	}()

	if err := flowCtrl.Run(ctx); err != nil {
		return fmt.Errorf("controller run: %w", err)
	}
	return nil
}
