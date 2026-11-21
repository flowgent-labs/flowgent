//go:build !linux

package seccomp

import (
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// Filter is a seccomp BPF filter; a no-op on non-Linux platforms.
type Filter struct{}

// ChildMain reports that the seccomp child process requires Linux.
func ChildMain() error { return errors.New("seccomp-child: seccomp is only supported on Linux") }

// BuildFilter returns an empty filter on non-Linux — seccomp is unsupported.
func BuildFilter(policy *model.NetworkPolicy) (*Filter, error) { return &Filter{}, nil }

// ScriptCmd runs a script without seccomp isolation on non-Linux.
func (f *Filter) ScriptCmd(scriptPath, runtimeName, workspace string) (*exec.Cmd, <-chan *Notifier, error) {
	extension := "sh"
	if runtimeName == "python3" {
		extension = "py"
	} else if runtimeName == "node" {
		extension = "js"
	}
	if runtimeName == "" {
		runtimeName = "bash"
	}
	cmd := exec.Command(runtimeName, filepath.Join(scriptPath, "script."+extension))
	cmd.Dir = scriptPath
	return cmd, nil, nil
}

// Notifier handles SECCOMP_USER_NOTIF; a no-op on non-Linux platforms.
type Notifier struct{}

// Start is a no-op.
func (n *Notifier) Start() {}

// Close is a no-op.
func (n *Notifier) Close() {}
