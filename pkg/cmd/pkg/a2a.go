package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	a2apkg "github.com/flowgent-labs/flowgent/a2a/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

func StartA2A(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startA2AService(cfgPath)
}

func StopA2A(pidFile string) error { return utils.StopByPID(pidFile) }

func RestartA2A(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return StartA2A(cfgPath, pidFile)
}

func startA2AService(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !cfg.A2A.Enabled {
		log.Println("A2A is disabled in config")
		utils.WaitSignal()
		return nil
	}

	slog.Info(fmt.Sprintf("Flowgent A2A Server starting (pid=%d)", os.Getpid()))

	srv := a2apkg.NewFlowgentA2AServer(cfg)
	return srv.Start(context.Background())
}
