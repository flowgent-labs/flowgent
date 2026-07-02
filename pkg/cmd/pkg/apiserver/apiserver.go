// Package apiserver provides the API server daemon entry point.
// The core logic lives in pkg/api/pkg/apiserver.go (FlowgentApiServer).
package apiserver

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

func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startServer(cfgPath)
}

func Stop(pidFile string) error { return utils.StopByPID(pidFile) }

func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return Start(cfgPath, pidFile)
}

func startServer(cfgPath string) error {
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
