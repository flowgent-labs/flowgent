package entities

import (
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// RuntimeMode selects the Flink-style runtime topology for a FlowRun.
type RuntimeMode string

const (
	RuntimeModeApplication RuntimeMode = "application"
	RuntimeModeSession     RuntimeMode = "session"
)

func ValidateRuntimeMode(mode RuntimeMode) error {
	switch mode {
	case RuntimeModeApplication, RuntimeModeSession:
		return nil
	default:
		return fmt.Errorf("runtime_mode must be %q or %q", RuntimeModeApplication, RuntimeModeSession)
	}
}

func RuntimeModeValue(mode RuntimeMode) string {
	return string(mode)
}

// RuntimeResources describes application-mode runtime pod resource overrides.
// Session-mode runtime resources are deployment-level concerns and are supplied
// by the Helm release instead of the Flow definition.
type RuntimeResources struct {
	JobManager  *model.SandboxResources `json:"jobmanager,omitempty" yaml:"jobmanager,omitempty"`
	TaskManager *model.SandboxResources `json:"taskmanager,omitempty" yaml:"taskmanager,omitempty"`
	Sandbox     *model.SandboxResources `json:"sandbox,omitempty" yaml:"sandbox,omitempty"`
}
