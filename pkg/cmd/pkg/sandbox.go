package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	sandboxpkg "github.com/flowgent-labs/flowgent/sandbox/pkg"
)

func StartSandbox(pidFile string) error {
	if pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	}
	return startSandboxService()
}

func StopSandbox(pidFile string) error {
	if pidFile == "" {
		return nil
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("read PID file %s: %w", pidFile, err)
	}
	pid, _ := strconv.Atoi(string(data))
	if pid > 0 {
		if p, e := os.FindProcess(pid); e == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
	os.Remove(pidFile)
	return nil
}

func RestartSandbox(pidFile string) error {
	_ = StopSandbox(pidFile)
	time.Sleep(500 * time.Millisecond)
	return StartSandbox(pidFile)
}

func startSandboxService() error {
	cfgPath := os.Getenv("FLOWGENT__CONFIG__FILE")
	if cfgPath == "" {
		cfgPath = "etc/flowgent.yaml"
	}
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	podName := os.Getenv("POD_NAME")
	if podName == "" {
		host, _ := os.Hostname()
		podName = fmt.Sprintf("sandbox-%s", host)
	}

	queue := messager.NewMessagerManager(svcCfg, podName)
	defer queue.Close()

	workspace := svcCfg.Sandbox.Workspace
	if workspace == "" {
		workspace = os.TempDir()
	}

	namespaceID := svcCfg.Runtime.Namespace.DefaultNamespace
	if namespaceID == "" {
		namespaceID = "default"
	}
	flowID := svcCfg.Runtime.AgentFlowID

	runner := sandboxpkg.NewFlowgentSandboxManager(podName, queue, "", workspace, svcCfg.Sandbox.Policy)
	runner.SetSlots(svcCfg.Sandbox.Deployment.SlotsPerPod)
	runner.SetScope(namespaceID, flowID)
	if svcCfg.Sandbox.Deployment.Enabled {
		runner.SetDistributed(true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	slog.Info("Sandbox worker starting",
		"pid", os.Getpid(), "pod", podName, "workspace", workspace,
		"slots", svcCfg.Sandbox.Deployment.SlotsPerPod,
		"namespace", namespaceID,
		"flow", flowID)
	return runner.Start(ctx)
}
