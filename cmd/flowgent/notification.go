package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
)

func runNotification(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent notification service starting (pid=%d)", os.Getpid())
		return startNotificationService()
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent notification service restarting (pid=%d)", os.Getpid())
		return startNotificationService()
	default:
		return fmt.Errorf("unknown notification action: %s", action)
	}
}

func startNotificationService() error {
	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	storeImpl := initStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	notifSvc := createNotificationService(storeImpl, serviceCfg)
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
