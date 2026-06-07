// Package a2a provides the A2A protocol server daemon entry point.
// It delegates to the apiserver package which runs the A2A server
// alongside the core engine (store, jobmanager, notifier).
package a2a

import (
	"time"

	"github.com/flowgent-labs/flowgent/cmd/src/apiserver"
	"github.com/flowgent-labs/flowgent/cmd/src/cmdutil"
)

// Start launches the A2A server daemon.
func Start(cfgPath, pidFile string) error {
	return apiserver.StartA2A(cfgPath, pidFile)
}

// Stop stops the A2A server daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the A2A server daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return apiserver.StartA2A(cfgPath, pidFile)
}
