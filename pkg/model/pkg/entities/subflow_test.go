package entities

import (
	"encoding/json"
	"testing"
)

func adaptiveState() *SubflowState {
	return &SubflowState{InstanceID: "repair", Revision: 1, Config: SubflowConfig{Ref: "repair-v1", Mode: "adaptive", Supervisor: "planner", Mutation: MutationPolicy{Allow: []string{"append", "replace_pending", "cancel_pending", "update_pending_edges", "seal"}}, Limits: FlowLimits{MaxCreatedNodes: 4, MaxPatches: 3}, Contract: FlowContract{RequiredNodes: []string{"verify"}}}, Graph: FlowInfo{Nodes: []Node{{ID: "work", Kind: NoopNode}, {ID: "verify", Kind: NoopNode}}, Edges: []Edge{{From: "work", To: "verify"}}}, CreatedNodes: 2}
}
func TestGraphPatchBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		ops    []PatchOperation
		status map[string]TaskStatus
	}{
		{"cycle", []PatchOperation{{Kind: "update_pending_edges", From: "verify", To: "work"}}, nil},
		{"parent reference", []PatchOperation{{Kind: "update_pending_edges", From: "outside", To: "work"}}, nil},
		{"running replacement", []PatchOperation{{Kind: "replace_pending", NodeID: "work", Node: &Node{ID: "work", Kind: NoopNode}}}, map[string]TaskStatus{"work": Running}},
		{"protected node", []PatchOperation{{Kind: "cancel_pending", NodeID: "verify"}}, nil},
		{"early seal", []PatchOperation{{Kind: "seal"}}, nil},
		{"seal failed contract", []PatchOperation{{Kind: "seal"}}, map[string]TaskStatus{"work": Success, "verify": Skipped}},
		{"append after seal", []PatchOperation{{Kind: "seal"}, {Kind: "append", Node: &Node{ID: "late", Kind: NoopNode}}}, map[string]TaskStatus{"work": Success, "verify": Success}},
		{"new capability", []PatchOperation{{Kind: "append", Node: &Node{ID: "payment", Kind: ToolNode, Tool: "payments"}}}, nil},
		{"duplicate identity", []PatchOperation{{Kind: "append", Node: &Node{ID: "work", Kind: NoopNode}}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := adaptiveState()
			before, _ := json.Marshal(s)
			_, err := s.Apply(GraphPatch{ID: "decision", SubflowInstanceID: s.InstanceID, ExpectedRevision: 1, Operations: tt.ops}, tt.status)
			if err == nil {
				t.Fatal("unsafe patch accepted")
			}
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("failed patch modified live state")
			}
		})
	}
}
func TestGraphPatchAppendAndSeal(t *testing.T) {
	s := adaptiveState()
	next, err := s.Apply(GraphPatch{ID: "append", SubflowInstanceID: s.InstanceID, ExpectedRevision: 1, Operations: []PatchOperation{{Kind: "append", Node: &Node{ID: "review", Kind: NoopNode}}, {Kind: "update_pending_edges", From: "work", To: "review"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 2 || len(s.Graph.Nodes) != 2 {
		t.Fatal("revision or isolation broken")
	}
	next, err = next.Apply(GraphPatch{ID: "seal", SubflowInstanceID: s.InstanceID, ExpectedRevision: 2, Operations: []PatchOperation{{Kind: "seal"}}}, map[string]TaskStatus{"work": Success, "verify": Success, "review": Success})
	if err != nil || !next.Sealed {
		t.Fatalf("seal: %v", err)
	}
}
func TestStaticSubflowDefaults(t *testing.T) {
	s := &SubflowConfig{Ref: "old-child"}
	if s.Adaptive() || s.Validate() != nil {
		t.Fatal("old config must be static")
	}
}
