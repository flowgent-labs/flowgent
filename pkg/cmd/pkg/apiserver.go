package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

func StartAPIServer(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startAPIServer(cfgPath)
}

func StopAPIServer(pidFile string) error { return utils.StopByPID(pidFile) }

func RestartAPIServer(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return StartAPIServer(cfgPath, pidFile)
}

func startAPIServer(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	slog.Info(fmt.Sprintf("Flowgent API Server starting (pid=%d)", os.Getpid()))

	srv, err := api.NewFlowgentApiServer(cfg)
	if err != nil {
		return fmt.Errorf("create API server: %w", err)
	}

	return srv.Start(context.Background())
}
