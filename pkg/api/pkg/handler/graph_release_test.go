package handler

import (
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestPortableAgentGraphSnapshotAndRuntimeBindings(t *testing.T) {
	source := &entities.AgentGraph{Name: "security-fixer",
		BaseEntity:   entities.BaseEntity{ID: "security-fixer", Namespace: "producer", Description: "shared"},
		K8sNamespace: "producer-runtime",
		RuntimeMode:  entities.RuntimeModeApplication,
		Nodes: []entities.GraphNode{
			{ID: "agent", Agent: "security-agent"},
			{ID: "map", Node: &entities.GraphNode{ID: "skill", Skill: "fix-skill"}},
			{ID: "subgraph", SubGraph: &entities.SubGraphConfig{Ref: "verifier"}},
		},
	}
	snapshot, err := portableAgentGraphSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Namespace != "" || snapshot.K8sNamespace != "" {
		t.Fatalf("producer runtime data leaked into snapshot: %+v", snapshot)
	}
	if snapshot.RuntimeMode != entities.RuntimeModeApplication {
		t.Fatalf("runtime mode was not preserved: %+v", snapshot.RuntimeMode)
	}
	if err := applyAgentGraphResourceBindings(snapshot, map[string]string{
		"agent:security-agent": "consumer-agent",
		"skill:fix-skill":      "consumer-skill",
		"graph:verifier":       "consumer-verifier",
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Nodes[0].Agent != "consumer-agent" || snapshot.Nodes[1].Node.Skill != "consumer-skill" || snapshot.Nodes[2].SubGraph.Ref != "consumer-verifier" {
		t.Fatalf("bindings were not applied: %+v", snapshot.Nodes)
	}
	if err := applyAgentGraphResourceBindings(snapshot, map[string]string{"agent:missing": "x"}); err == nil {
		t.Fatal("unused binding was accepted")
	}
}
