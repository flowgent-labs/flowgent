package utils

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// WritePID writes the current process PID to a file.
func WritePID(pidFile string) {
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// StopByPID reads a PID file and sends SIGTERM to the process.
func StopByPID(pidFile string) error {
	if pidFile == "" {
		return nil
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("read PID file %s: %w (is the service running?)", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID file %s: %w", pidFile, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM to %d: %w", pid, err)
	}
	fmt.Printf("Sent SIGTERM to process %d (pidfile=%s)\n", pid, pidFile)
	os.Remove(pidFile)
	return nil
}

// WaitSignal blocks until SIGTERM or SIGINT.
func WaitSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
}

// Hostname returns the hostname or "unknown".
func Hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "unknown"
	}
	return h
}
