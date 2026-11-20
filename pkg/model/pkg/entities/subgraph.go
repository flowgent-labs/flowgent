package entities

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// SubGraphConfig extends the existing agentgraph node. An omitted mode is static.
// Ref names a published AgentGraph in the same namespace.
type SubGraphConfig struct {
	DefinitionSnapshot   *AgentGraph        `json:"definition_snapshot,omitempty" yaml:"-"`
	DefinitionRevisionID string             `json:"definition_revision_id,omitempty" yaml:"-"`
	Ref                  string             `json:"ref" yaml:"ref"`
	Mode                 string             `json:"mode,omitempty" yaml:"mode,omitempty"`
	Supervisor           string             `json:"supervisor,omitempty" yaml:"supervisor,omitempty"`
	Contract             AgentGraphContract `json:"contract,omitempty" yaml:"contract,omitempty"`
	PolicyRef            string             `json:"policy_ref,omitempty" yaml:"policy_ref,omitempty"`
	Mutation             MutationPolicy     `json:"mutation,omitempty" yaml:"mutation,omitempty"`
	Safepoint            SafepointConfig    `json:"safepoint,omitempty" yaml:"safepoint,omitempty"`
	Limits               AgentGraphLimits   `json:"limits,omitempty" yaml:"limits,omitempty"`
	IndependentRun       bool               `json:"independent_run,omitempty" yaml:"independent_run,omitempty"`
}
type AgentGraphContract struct {
	InputSchemaRef      string   `json:"input_schema_ref,omitempty" yaml:"input_schema_ref,omitempty"`
	OutputSchemaRef     string   `json:"output_schema_ref,omitempty" yaml:"output_schema_ref,omitempty"`
	CompletionPolicyRef string   `json:"completion_policy_ref,omitempty" yaml:"completion_policy_ref,omitempty"`
	RequiredNodes       []string `json:"required_nodes,omitempty" yaml:"required_nodes,omitempty"`
}
type MutationPolicy struct {
	Allow []string `json:"allow" yaml:"allow"`
}
type SafepointConfig struct {
	Timeout   string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	OnTimeout string `json:"on_timeout,omitempty" yaml:"on_timeout,omitempty"`
}
type AgentGraphLimits struct {
	MaxCreatedNodes   int    `json:"max_created_nodes,omitempty" yaml:"max_created_nodes,omitempty"`
	MaxParallelism    int    `json:"max_parallelism,omitempty" yaml:"max_parallelism,omitempty"`
	MaxPatches        int    `json:"max_patches,omitempty" yaml:"max_patches,omitempty"`
	MaxDepth          int    `json:"max_depth,omitempty" yaml:"max_depth,omitempty"`
	MaxActiveDuration string `json:"max_active_duration,omitempty" yaml:"max_active_duration,omitempty"`
}

func (s *SubGraphConfig) Adaptive() bool { return s != nil && s.Mode == "adaptive" }
func (s *SubGraphConfig) Validate() error {
	if s == nil {
		return nil
	}
	if s.Ref == "" {
		return fmt.Errorf("subgraph.ref is required")
	}
	if s.Mode != "" && s.Mode != "static" && s.Mode != "adaptive" {
		return fmt.Errorf("invalid subgraph mode %q", s.Mode)
	}
	if s.Adaptive() && s.Supervisor == "" {
		return fmt.Errorf("adaptive subgraph requires supervisor")
	}
	if s.Limits.MaxCreatedNodes < 0 || s.Limits.MaxParallelism < 0 || s.Limits.MaxPatches < 0 || s.Limits.MaxDepth < 0 {
		return fmt.Errorf("subgraph limits cannot be negative")
	}
	for _, d := range []string{s.Safepoint.Timeout, s.Limits.MaxActiveDuration} {
		if d != "" {
			if v, e := time.ParseDuration(d); e != nil || v <= 0 {
				return fmt.Errorf("invalid positive duration %q", d)
			}
		}
	}
	if s.Safepoint.OnTimeout != "" && s.Safepoint.OnTimeout != "defer_patch" {
		return fmt.Errorf("unsupported safepoint timeout action")
	}
	for _, op := range s.Mutation.Allow {
		if !slices.Contains([]string{"append", "replace_pending", "cancel_pending", "update_pending_edges", "seal"}, op) {
			return fmt.Errorf("unsupported mutation %q", op)
		}
	}
	return nil
}

// SubGraph is stored in the existing run metadata and append-only event log.
// GraphNode IDs are never reused within an instance, including cancelled nodes.
type SubGraph struct {
	InstanceID   string            `json:"instance_id"`
	Revision     GraphRevision     `json:"revision"`
	Config       SubGraphConfig    `json:"config"`
	Graph        AgentGraph        `json:"graph"`
	Sealed       bool              `json:"sealed"`
	CreatedNodes int               `json:"created_nodes"`
	Patches      int               `json:"patches"`
	Templates    []GraphNode       `json:"templates,omitempty"`
	Decisions    map[string]string `json:"decisions,omitempty"`
}
type GraphPatch struct {
	ID                 string           `json:"id"`
	GraphRunID         string           `json:"graph_run_id"`
	SubGraphInstanceID string           `json:"subgraph_instance_id"`
	ExpectedRevision   GraphRevision    `json:"expected_revision"`
	Operations         []PatchOperation `json:"operations"`
}
type PatchOperation struct {
	Kind           string     `json:"kind"`
	NodeInstanceID string     `json:"node_instance_id,omitempty"`
	Node           *GraphNode `json:"node,omitempty"`
	From           string     `json:"from,omitempty"`
	To             string     `json:"to,omitempty"`
	Remove         bool       `json:"remove,omitempty"`
}

// Apply builds a detached candidate. The API commits it only after a run-row CAS.
// Dispatched/running work is conservatively deferred; no timeout implies safety.
func (s *SubGraph) Apply(p GraphPatch, status map[string]NodeRunStatus) (*SubGraph, error) {
	if !s.Config.Adaptive() {
		return nil, fmt.Errorf("static subgraph cannot be patched")
	}
	if p.ID == "" || p.SubGraphInstanceID != s.InstanceID || p.ExpectedRevision != s.Revision {
		return nil, fmt.Errorf("graph revision conflict")
	}
	if s.Sealed {
		return nil, fmt.Errorf("subgraph is sealed")
	}
	if len(p.Operations) == 0 {
		return nil, fmt.Errorf("empty graph patch")
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var n SubGraph
	if err = json.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	if n.Config.Limits.MaxPatches > 0 && n.Patches >= n.Config.Limits.MaxPatches {
		return nil, fmt.Errorf("patch budget exhausted")
	}
	index := func(id string) int {
		for i, v := range n.Graph.Nodes {
			if v.ID == id {
				return i
			}
		}
		return -1
	}
	pending := func(id string) bool { return status[id] == "" || status[id] == NodeRunPending }
	for _, op := range p.Operations {
		if n.Sealed {
			return nil, fmt.Errorf("seal must be the final patch operation")
		}
		if !slices.Contains(n.Config.Mutation.Allow, op.Kind) {
			return nil, fmt.Errorf("mutation %q is not allowed", op.Kind)
		}
		switch op.Kind {
		case "append":
			if op.Node == nil || index(op.Node.ID) >= 0 || status[op.Node.ID] != "" {
				return nil, fmt.Errorf("new node identity required")
			}
			if err := ValidateGraphNode(op.Node); err != nil {
				return nil, err
			}
			if err := s.checkCapability(op.Node); err != nil {
				return nil, err
			}
			n.Graph.Nodes = append(n.Graph.Nodes, *op.Node)
			n.CreatedNodes++
		case "replace_pending":
			i := index(op.NodeInstanceID)
			if i < 0 || !pending(op.NodeInstanceID) {
				return nil, fmt.Errorf("defer_patch: node is not pending")
			}
			if slices.Contains(n.Config.Contract.RequiredNodes, op.NodeInstanceID) {
				return nil, fmt.Errorf("protected node")
			}
			if op.Node == nil || op.Node.ID != op.NodeInstanceID {
				return nil, fmt.Errorf("replacement must preserve identity")
			}
			if err := s.checkCapability(op.Node); err != nil {
				return nil, err
			}
			n.Graph.Nodes[i] = *op.Node
		case "cancel_pending":
			i := index(op.NodeInstanceID)
			if i < 0 || !pending(op.NodeInstanceID) {
				return nil, fmt.Errorf("defer_patch: node is not pending")
			}
			if slices.Contains(n.Config.Contract.RequiredNodes, op.NodeInstanceID) {
				return nil, fmt.Errorf("protected node")
			}
			// Keep the identity as a noop; attempts and audit history are never deleted.
			n.Graph.Nodes[i] = GraphNode{ID: op.NodeInstanceID, Kind: NoopNode}
		case "update_pending_edges":
			if index(op.From) < 0 || index(op.To) < 0 || !pending(op.To) {
				return nil, fmt.Errorf("edge endpoint invalid or already dispatched")
			}
			if slices.Contains(n.Config.Contract.RequiredNodes, op.To) {
				return nil, fmt.Errorf("protected dependency")
			}
			n.Graph.Edges = slices.DeleteFunc(n.Graph.Edges, func(e GraphEdge) bool { return e.From == op.From && e.To == op.To })
			if !op.Remove {
				n.Graph.Edges = append(n.Graph.Edges, GraphEdge{From: op.From, To: op.To})
			}
		case "seal":
			for _, node := range n.Graph.Nodes {
				if status[node.ID] != NodeRunSucceeded && status[node.ID] != NodeRunSkipped {
					return nil, fmt.Errorf("cannot seal unfinished node %q", node.ID)
				}
			}
			for _, id := range n.Config.Contract.RequiredNodes {
				if status[id] != NodeRunSucceeded {
					return nil, fmt.Errorf("completion contract not satisfied: %s", id)
				}
			}
			n.Sealed = true
		}
	}
	if n.Config.Limits.MaxCreatedNodes > 0 && n.CreatedNodes > n.Config.Limits.MaxCreatedNodes {
		return nil, fmt.Errorf("node budget exhausted")
	}
	if err := n.Graph.ValidateAcyclic(); err != nil {
		return nil, err
	}
	n.Revision++
	n.Patches++
	return &n, nil
}

// ValidateAcyclic is strict for new adaptive graphs. Legacy static conditional
// feedback edges retain their existing execution semantics.
func (f *AgentGraph) ValidateAcyclic() error {
	if err := f.ValidateDAG(); err != nil {
		return err
	}
	degree := map[string]int{}
	children := map[string][]string{}
	for _, n := range f.Nodes {
		degree[n.ID] = 0
	}
	seen := map[[2]string]bool{}
	for _, e := range f.Edges {
		k := [2]string{e.From, e.To}
		if seen[k] {
			return fmt.Errorf("duplicate edge")
		}
		seen[k] = true
		degree[e.To]++
		children[e.From] = append(children[e.From], e.To)
	}
	var ready []string
	for id, d := range degree {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	count := 0
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		count++
		for _, c := range children[id] {
			degree[c]--
			if degree[c] == 0 {
				ready = append(ready, c)
			}
		}
	}
	if count != len(f.Nodes) {
		return fmt.Errorf("adaptive graph contains a cycle")
	}
	return nil
}

// Dynamic tasks may vary instructions and arguments, but cannot acquire a new
// execution capability. Initial definitions are the capability templates until
// a policy resolver is explicitly installed. Sandbox isolation options remain
// part of the immutable capability, while its script is noderun-specific.
func (s *SubGraph) checkCapability(node *GraphNode) error {
	if node.Kind == NoopNode || node.Kind == JoinNode {
		return nil
	}
	normalize := func(value GraphNode) string {
		value.ID = ""
		value.GraphNodeID = ""
		value.Args = nil
		value.Instruction = ""
		value.OutputSchema = nil
		value.Script = ""
		data, _ := json.Marshal(value)
		return string(data)
	}
	templates := s.Templates
	if templates == nil {
		templates = s.Graph.Nodes
	}
	for _, template := range templates {
		if normalize(template) == normalize(*node) {
			return nil
		}
	}
	return fmt.Errorf("dynamic node requests capability outside initial subgraph templates")
}
