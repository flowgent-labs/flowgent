// Package a2a provides the A2A protocol server daemon entry point.
// The core logic lives in pkg/a2a/pkg/a2a.go (FlowgentA2AServer).
package a2a

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

func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startService(cfgPath)
}

func Stop(pidFile string) error { return utils.StopByPID(pidFile) }

func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return Start(cfgPath, pidFile)
}

func startService(cfgPath string) error {
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
