package handler

import (
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestPortableFlowSnapshotAndResourceBindings(t *testing.T) {
	source := &entities.FlowInfo{
		BaseEntity:     entities.BaseEntity{ID: "security-fixer", Namespace: "producer", Description: "shared"},
		K8sNamespace:   "producer-runtime",
		Credentials:    map[string]string{"github": "producer-secret"},
		ResourcePoolID: "default",
		Nodes: []entities.Node{
			{ID: "agent", Agent: "security-agent"},
			{ID: "map", Node: &entities.Node{ID: "skill", Skill: "fix-skill"}},
			{ID: "subflow", AgentFlowID: "verifier"},
		},
	}
	snapshot, err := portableFlowSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Namespace != "" || snapshot.K8sNamespace != "" || snapshot.Credentials != nil {
		t.Fatalf("producer runtime data leaked into snapshot: %+v", snapshot)
	}
	if err := applyFlowResourceBindings(snapshot, map[string]string{
		"agent:security-agent": "consumer-agent",
		"skill:fix-skill":      "consumer-skill",
		"flow:verifier":        "consumer-verifier",
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Nodes[0].Agent != "consumer-agent" || snapshot.Nodes[1].Node.Skill != "consumer-skill" || snapshot.Nodes[2].AgentFlowID != "consumer-verifier" {
		t.Fatalf("bindings were not applied: %+v", snapshot.Nodes)
	}
	if err := applyFlowResourceBindings(snapshot, map[string]string{"agent:missing": "x"}); err == nil {
		t.Fatal("unused binding was accepted")
	}
}
