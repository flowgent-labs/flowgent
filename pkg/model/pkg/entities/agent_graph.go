package entities

import (
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// AgentGraph contains the full specification of a graph (nodes, edges, triggers, etc.).
type AgentGraph struct {
	BaseEntity

	// Name is the namespace-local resource name used by manifests and HTTP
	// routes. BaseEntity.ID is the stable, globally unique database identity.
	// ResourceName accepts the pre-revision API shape where name was carried in
	// id, so transport compatibility does not leak into persistence.
	Name             string                       `json:"name,omitempty" yaml:"name,omitempty" db:"name"`
	Revision         int64                        `json:"revision,omitempty" yaml:"-" db:"-"`
	SummarizeEnabled bool                         `json:"summarize_enabled" yaml:"summarize_enabled" db:"-"`
	Kind             string                       `json:"kind,omitempty" yaml:"kind,omitempty"`
	Summary          string                       `json:"summary,omitempty" yaml:"summary,omitempty"`
	InputSchema      map[string]any               `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema     map[string]any               `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Vars             map[string]any               `json:"vars,omitempty" yaml:"vars,omitempty"`
	Nodes            []GraphNode                  `json:"nodes" yaml:"nodes"`
	Edges            []GraphEdge                  `json:"edges" yaml:"edges"`
	Triggers         []TriggerDef                 `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	SandboxPolicy    *model.SandboxPolicyOverride `json:"sandbox_policy,omitempty" yaml:"sandbox_policy,omitempty"`

	RuntimeMode RuntimeMode       `json:"runtime_mode" yaml:"runtime_mode"`
	Resources   *RuntimeResources `json:"resources,omitempty" yaml:"resources,omitempty"`

	K8sNamespace string            `json:"k8s_namespace,omitempty" yaml:"namespace,omitempty"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// ResourceName is the namespace-local AgentGraph name. Stable IDs are assigned
// by the repository and are never accepted as an implicit authoring name.
func (g *AgentGraph) ResourceName() string {
	if g == nil {
		return ""
	}
	return g.Name
}

// ValidateDAG verifies node identities, kinds, and edge references before a
// definition can enter persistent storage or runtime scheduling.
func (f *AgentGraph) ValidateDAG() error {
	nodes := make(map[string]struct{}, len(f.Nodes))
	for i := range f.Nodes {
		if err := ValidateGraphNode(&f.Nodes[i]); err != nil {
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

type AgentGraphWatchResponse struct {
	AgentGraphs []AgentGraph `json:"agent_graphs"`
	Cursor      int64        `json:"cursor"`
}

// TriggerDef defines a trigger for a graph (schedule or webhook).
type TriggerDef struct {
	Type     string   `json:"type" yaml:"type"`
	Cron     string   `json:"cron,omitempty" yaml:"cron,omitempty"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Events   []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// AgentGraphRevision represents a versioned graph stored in the database.
type AgentGraphRevision struct {
	BaseEntity

	AgentGraphID     string `json:"agent_graph_id" db:"agent_graph_id"`
	AgentGraphName   string `json:"agent_graph_name" db:"agent_graph_name"`
	Revision         int64  `json:"revision" db:"revision"`
	Definition       []byte `json:"definition"`
	SummarizeEnabled bool   `json:"summarize_enabled"`
	Checksum         string `json:"checksum"`
	Comment          string `json:"comment,omitempty"`
}
