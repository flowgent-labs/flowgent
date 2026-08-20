package entities

import (
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// FlowInfo contains the full specification of a flow (nodes, edges, triggers, etc.).
type FlowInfo struct {
	BaseEntity

	Version       int64                        `json:"version,omitempty" yaml:"-"`
	Kind          string                       `json:"kind,omitempty" yaml:"kind,omitempty"`
	Summary       string                       `json:"summary,omitempty" yaml:"summary,omitempty"`
	InputSchema   map[string]any               `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema  map[string]any               `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Vars          map[string]any               `json:"vars,omitempty" yaml:"vars,omitempty"`
	Nodes         []Node                       `json:"nodes" yaml:"nodes"`
	Edges         []Edge                       `json:"edges" yaml:"edges"`
	Triggers      []TriggerDef                 `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	SandboxPolicy *model.SandboxPolicyOverride `json:"sandbox_policy,omitempty" yaml:"sandbox_policy,omitempty"`

	RuntimeMode RuntimeMode       `json:"runtime_mode" yaml:"runtime_mode"`
	Resources   *RuntimeResources `json:"resources,omitempty" yaml:"resources,omitempty"`

	K8sNamespace string            `json:"k8s_namespace,omitempty" yaml:"namespace,omitempty"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// ValidateDAG verifies node identities, kinds, and edge references before a
// definition can enter persistent storage or runtime scheduling.
func (f *FlowInfo) ValidateDAG() error {
	nodes := make(map[string]struct{}, len(f.Nodes))
	for i := range f.Nodes {
		if err := ValidateNode(&f.Nodes[i]); err != nil {
			return err
		}
		if _, exists := nodes[f.Nodes[i].ID]; exists {
			return fmt.Errorf("duplicate node id %q", f.Nodes[i].ID)
		}
		nodes[f.Nodes[i].ID] = struct{}{}
	}
	for _, edge := range f.Edges {
		if _, ok := nodes[edge.From]; !ok {
			return fmt.Errorf("edge references unknown source node %q", edge.From)
		}
		if _, ok := nodes[edge.To]; !ok {
			return fmt.Errorf("edge references unknown target node %q", edge.To)
		}
	}
	return nil
}

type FlowWatchResponse struct {
	Flows   []FlowInfo `json:"flows"`
	Version int64      `json:"version"`
}

// TriggerDef defines a trigger for a flow (schedule or webhook).
type TriggerDef struct {
	Type     string   `json:"type" yaml:"type"`
	Cron     string   `json:"cron,omitempty" yaml:"cron,omitempty"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Events   []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// FlowVersionInfo represents a versioned flow stored in the database.
type FlowVersionInfo struct {
	BaseEntity

	FlowID     string `json:"flow_id" db:"agentflow_id"`
	Version    int64  `json:"version"`
	Definition []byte `json:"definition"`
	Comment    string `json:"comment,omitempty"`
}
