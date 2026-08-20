package console

import (
	"encoding/json"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const ConsoleAPIVersion = "core.flowgent.io/v1"

// ExportData is the root structure for import/export of all resources.
type ExportData struct {
	LLMs       []entities.LlmProviderInfo   `json:"llms" yaml:"llms"`
	Channels   []entities.NotifyChannelInfo `json:"channels" yaml:"channels"`
	MCPs       []entities.McpInfo           `json:"mcps" yaml:"mcps"`
	Skills     []entities.FlowInfo          `json:"skills" yaml:"skills"`
	AgentDefs  []entities.AgentInfo         `json:"agentDefs" yaml:"agentDefs"`
	AgentFlows []entities.FlowInfo          `json:"agentFlows" yaml:"agentFlows"`
	FlowRuns   []entities.FlowRunInfo       `json:"flowRuns" yaml:"flowRuns"`
}

// ResourceMetadata holds the metadata block common to all console resources.
type ResourceMetadata struct {
	Name        string            `json:"name" yaml:"name"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Status      string            `json:"status,omitempty" yaml:"status,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
}

// ResourceImport is the canonical single-resource envelope:
//
//	consoleVersion: core.flowgent.io/v1
//	kind: Agent|Flow|FlowRun|MCP|LLMProvider|NotifyChannel|Skill
//	metadata:
//	  name: xxx
//	  namespace: default
//	data: {...}
type ResourceImport struct {
	ConsoleVersion string            `json:"consoleVersion" yaml:"consoleVersion"`
	Kind           string            `json:"kind" yaml:"kind"`
	Metadata       *ResourceMetadata `json:"metadata" yaml:"metadata"`
	Data           json.RawMessage   `json:"data" yaml:"data"`
}
