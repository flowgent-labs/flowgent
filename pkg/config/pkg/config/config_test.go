package config

import (
	"testing"

	"github.com/flowgent-labs/flowgent/model/src"
)

func TestAppConfig_Helpers(t *testing.T) {
	cfg := &AppConfig{
		Service: FlowgentConfig{
			Orchestration: OrchestrationConfig{
				Agents: ResourceCfg{
					Static: StaticResourceCfg{Enabled: true, LoadDir: "agents/", Refresh: "30s"},
				},
				MCPs: []MCPDef{{Name: "mcp1", Enabled: true}},
			},
		},
		Agents: []AgentDef{{Name: "a1", Model: "m1"}},
		Flows:  []model.AgentFlowSpec{{ID: "f1"}},
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
