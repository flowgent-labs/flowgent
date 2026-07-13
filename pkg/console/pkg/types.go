package console

import (
	"encoding/json"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ExportData is the root structure for import/export of all resources.
type ExportData struct {
	LLMs       []entities.LlmProviderInfo   `json:"llms" yaml:"llms"`
	Channels   []entities.NotifyChannelInfo `json:"channels" yaml:"channels"`
	MCPs       []entities.McpInfo           `json:"mcps" yaml:"mcps"`
	Skills     []entities.FlowInfo          `json:"skills" yaml:"skills"`
	AgentDefs  []entities.AgentInfo         `json:"agentDefs" yaml:"agentDefs"`
	AgentFlows []entities.FlowInfo          `json:"agentFlows" yaml:"agentFlows"`
	FlowRuns   []entities.FlowRunInfo       `json:"flowRuns" yaml:"flowRuns"`
	Wallets    []WalletExport               `json:"wallets" yaml:"wallets"`
}

// WalletExport is the exported form of a wallet key.
type WalletExport struct {
	Name       string `json:"name" yaml:"name"`
	PrivateKey string `json:"private_key" yaml:"private_key"`
}

// ResourceMetadata holds the metadata block common to all K8s-style resources.
type ResourceMetadata struct {
	Name        string            `json:"name" yaml:"name"`
	Tenant      string            `json:"tenant,omitempty" yaml:"tenant,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Status      string            `json:"status,omitempty" yaml:"status,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
}

// ResourceImport is a K8s-style single-resource wrapper:
//
//	apiVersion: console.flowgent.io/v1
//	kind: Agent|Flow|FlowRun|MCP|LLMProvider|NotifyChannel|Skill
//	metadata:
//	  name: xxx
//	  tenant: default
//	spec: {...}
type ResourceImport struct {
	APIVersion string            `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   *ResourceMetadata `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       json.RawMessage   `json:"spec" yaml:"spec"`
}
