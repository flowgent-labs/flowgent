package workflow

import (
	"context"
	"github.com/cyberbot/cve-auto-fix/src/internal/adk/tools"
)

type Agent struct {
	ID           string         `yaml:"id"`
	Model        string         `yaml:"model"`
	Temperature  float64        `yaml:"temperature"`
	SystemPrompt string         `yaml:"system_prompt"`
	ToolRefs     []string       `yaml:"toolrefs"`
}

type ConfigLoader interface {
	Load(path string) (*WorkflowConfig, error)
}

type WorkflowRunner interface {
	Run(ctx context.Context, workflowID string, input map[string]any) (string, error)
}

type WorkflowConfig struct {
	Toolsets  map[string][]tools.Tool    `yaml:"toolsets"`
	AgentSets map[string]Agent           `yaml:"agentsets"`
	Workflows map[string][]WorkflowTask  `yaml:"workflows"`
}

type WorkflowTask struct {
	Type      string                   `yaml:"type"`
	Tasks     []WorkflowTask           `yaml:"tasks,omitempty"`
	AgentRef  string                   `yaml:"agent_ref,omitempty"`
	ToolRef   string                   `yaml:"tool_ref,omitempty"`
	Params    map[string]any           `yaml:"params"`
}
