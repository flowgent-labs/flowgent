//go:build !linux

package seccomp

import (
	"os/exec"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// Filter is a seccomp BPF filter; a no-op on non-Linux platforms.
type Filter struct{}

// BuildFilter returns an empty filter on non-Linux — seccomp is unsupported.
func BuildFilter(policy *model.NetworkPolicy) (*Filter, error) { return &Filter{}, nil }

// ScriptCmd runs a script without seccomp isolation on non-Linux.
func (f *Filter) ScriptCmd(scriptPath, runtimeName, workspace string) (*exec.Cmd, <-chan *Notifier, error) {
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = workspace
	return cmd, nil, nil
}

// Notifier handles SECCOMP_USER_NOTIF; a no-op on non-Linux platforms.
type Notifier struct{}

// Start is a no-op.
func (n *Notifier) Start() {}

// Close is a no-op.
func (n *Notifier) Close() {}
