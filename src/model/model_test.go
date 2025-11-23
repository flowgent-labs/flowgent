package model

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNodeTypeConstants(t *testing.T) {
	types := map[NodeType]string{
		AgentNode: "agent", ToolNode: "tool", MapNode: "map",
		AgentFlowNode: "agentflow", ConditionNode: "condition",
		TribunalNode: "tribunal", HumanNode: "human",
		SupervisorNode: "supervisor", NoopNode: "noop",
	}
	for nt, expected := range types {
		if string(nt) != expected {
			t.Errorf("%s expected %q, got %q", nt, expected, string(nt))
		}
	}
}

func TestRunStatusConstants(t *testing.T) {
	if string(RunPending) != "PENDING" {
		t.Error("RunPending mismatch")
	}
	if string(RunRunning) != "RUNNING" {
		t.Error("RunRunning mismatch")
	}
}

func TestTaskStatusConstants(t *testing.T) {
	if string(Success) != "SUCCESS" {
		t.Error("Success mismatch")
	}
	if string(WaitingHuman) != "WAITING_HUMAN" {
		t.Error("WaitingHuman mismatch")
	}
}

func TestMemoryTypeConstants(t *testing.T) {
	if MemoryEpisodic != "episodic" {
		t.Error("MemoryEpisodic mismatch")
	}
}

func TestAgentFlowSpec_UnmarshalFlat(t *testing.T) {
	yamlData := `
id: test-flow
description: test desc
nodes:
  - id: n1
    type: agent
    agent: a1
edges:
  - from: n1
    to: n2
`
	var spec AgentFlowSpec
	if err := yaml.Unmarshal([]byte(yamlData), &spec); err != nil {
		t.Fatalf("unmarshal flat: %v", err)
	}
	if spec.ID != "test-flow" {
		t.Errorf("expected test-flow, got %s", spec.ID)
	}
	if spec.Description != "test desc" {
		t.Errorf("expected 'test desc', got %s", spec.Description)
	}
	if len(spec.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(spec.Nodes))
	}
	if len(spec.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(spec.Edges))
	}
}


func TestAgentFlowSpec_Triggers(t *testing.T) {
	yamlData := `
id: triggered-flow
nodes: []
edges: []
triggers:
  - type: schedule
    cron: "0 */6 * * *"
  - type: webhook
    provider: github
    events: [push, pull_request]
`
	var spec AgentFlowSpec
	if err := yaml.Unmarshal([]byte(yamlData), &spec); err != nil {
		t.Fatalf("unmarshal triggers: %v", err)
	}
	if len(spec.Triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(spec.Triggers))
	}
	if spec.Triggers[0].Cron != "0 */6 * * *" {
		t.Errorf("cron mismatch: %s", spec.Triggers[0].Cron)
	}
}

func TestRetryPolicy(t *testing.T) {
	rp := RetryPolicy{Max: 5, Initial: 1e9, MaxDelay: 3e10, Factor: 2.0}
	if rp.Max != 5 {
		t.Error("Max mismatch")
	}
}

func TestSupervisorConfig(t *testing.T) {
	sc := SupervisorConfig{
		MaxInjections: 5, MaxNodes: 50, AllowedActions: []string{"continue", "abort"},
	}
	if len(sc.AllowedActions) != 2 {
		t.Error("allowed actions mismatch")
	}
}

func TestAppConfig_Helpers(t *testing.T) {
	cfg := &AppConfig{
		Service: ServiceConfig{
			Orchestration: OrchestrationConfig{
				Agents: []AgentDef{{Name: "a1", Model: "m1"}},
				MCPs:   []MCPDef{{Name: "mcp1", Enabled: true}},
			},
		},
		Flows: []AgentFlowSpec{{ID: "f1"}},
	}

	if cfg.GetAgent("a1") == nil {
		t.Error("GetAgent should find a1")
	}
	if cfg.GetAgent("missing") != nil {
		t.Error("GetAgent should return nil")
	}
	if cfg.GetMCP("mcp1") == nil {
		t.Error("GetMCP should find mcp1")
	}
	if cfg.GetFlow("f1") == nil {
		t.Error("GetFlow should find f1")
	}
}
