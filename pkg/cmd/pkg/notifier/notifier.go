// Package notifier provides the Notifier service daemon entry point.
// The Notifier service handles multi-channel push notifications
// (WebSocket, Slack, Telegram, Email) for agentflow events.
package notifier

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/cmd/pkg/cmdutil"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
)

// Start launches the Notifier daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		if err := os.WriteFile(pidFile, []byte{}, 0644); err != nil {
			return err
		}
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	log.Printf("Flowgent notification service starting (pid=%d)", os.Getpid())
	return startService(cfgPath)
}

// Stop stops the Notifier daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the Notifier daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	log.Printf("Flowgent notification service restarting (pid=%d)", os.Getpid())
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

	apiClient := client.NewFlowgentClient()
	notifSvc := cmdutil.CreateNotifierService(apiClient, serviceCfg)
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
