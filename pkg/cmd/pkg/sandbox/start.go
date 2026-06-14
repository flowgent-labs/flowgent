// Package sandbox provides the sandbox daemon entry point and the secure script
// execution worker (SandboxRunner). Start/Stop/Restart are self-contained to
// avoid circular dependency with the internal package.
package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
)

// Start launches the sandbox worker daemon.
func Start(pidFile string) error {
	if pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	}
	return startService()
}

// Stop stops the sandbox worker daemon.
func Stop(pidFile string) error {
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

// Restart restarts the sandbox worker daemon.
func Restart(pidFile string) error {
	_ = Stop(pidFile)
	time.Sleep(500 * time.Millisecond)
	return Start(pidFile)
}

// startService is the actual sandbox worker startup logic.
func startService() error {
	cfgPath := os.Getenv("FLOWGENT__CONFIG__FILE")
	if cfgPath == "" {
		cfgPath = "etc/flowgent-dev.yaml"
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

	runner := NewSandboxRunner(podName, queue, "", workspace, svcCfg.Sandbox.Policy)
	if svcCfg.Deployment.Mode != "" || svcCfg.Sandbox.Deployment.Enabled {
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

	log.Printf("Sandbox worker starting (pid=%d, pod=%s, workspace=%s)", os.Getpid(), podName, workspace)
	return runner.Start(ctx)
}
