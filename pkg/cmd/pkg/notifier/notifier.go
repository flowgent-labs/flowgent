// Package notifier provides the Notifier service daemon entry point.
// The Notifier service handles multi-channel push notifications
// (WebSocket, Slack, Telegram, Email) for agentflow events.
package notifier

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	notifierpkg "github.com/flowgent-labs/flowgent/notifier/pkg"
)

// Start launches the Notifier daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		if err := os.WriteFile(pidFile, []byte{}, 0644); err != nil {
			return err
		}
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	slog.Info("Flowgent notification service starting", "pid", os.Getpid())
	return startService(cfgPath)
}

// Stop stops the Notifier daemon.
func Stop(pidFile string) error {
	return utils.StopByPID(pidFile)
}

// Restart restarts the Notifier daemon.
func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	slog.Info("Flowgent notification service restarting", "pid", os.Getpid())
	return startService(cfgPath)
}

// startService is the actual notifier startup logic.
func startService(cfgPath string) error {
	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	if !serviceCfg.Notifier.Enabled {
		log.Println("Notification service is disabled in config")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		return nil
	}

	apiClient := client.NewFlowgentClient(serviceCfg.Runtime.APIServerURL)
	httpClient := client.NewHttpClient(serviceCfg, nil)
	notifSvc := notifierpkg.CreateNotifierService(apiClient, serviceCfg, httpClient)
	if notifSvc == nil {
		log.Println("Notification service is disabled in config")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		return nil
	}
	defer notifSvc.Shutdown()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Notification service shutting down")
	return nil
}
