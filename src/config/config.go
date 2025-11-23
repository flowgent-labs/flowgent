package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/flowgent-labs/flowgent/src/model"
)

// Load reads the main service config file.
func Load(path string) (*model.ServiceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg model.ServiceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// LoadAgentFlows discovers and loads all L2 agentflow YAML files.
func LoadAgentFlows(cfg *model.ServiceConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	cfgDir := filepath.Dir(cfgPath)
	var flows []model.AgentFlowSpec
	subFlows := make(map[string]model.AgentFlowSpec)

	if cfg.Orchestration.AgentFlows.Static.Enabled {
		for _, p := range cfg.Orchestration.AgentFlows.Static.Paths {
			fullPath := filepath.Join(cfgDir, p)
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				entries, _ := os.ReadDir(fullPath)
				for _, e := range entries {
					if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
						loadAgentFlowFile(filepath.Join(fullPath, e.Name()), &flows, subFlows)
					}
				}
			} else {
				loadAgentFlowFile(fullPath, &flows, subFlows)
			}
		}
	}

	return flows, subFlows, nil
}

func loadAgentFlowFile(path string, flows *[]model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var spec model.AgentFlowSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return
	}
	if spec.ID == "" {
		return
	}
	if filepath.Base(filepath.Dir(path)) == "sub" {
		subFlows[spec.ID] = spec
	} else {
		*flows = append(*flows, spec)
	}
}

// ReloadAgentFlows re-reads agentflow YAML files (for hot reload).
func ReloadAgentFlows(cfg *model.ServiceConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	return LoadAgentFlows(cfg, cfgPath)
}

// BuildAppConfig combines service config with loaded flows.
func BuildAppConfig(cfg *model.ServiceConfig, flows []model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) *model.AppConfig {
	return &model.AppConfig{
		Service:  *cfg,
		Flows:    flows,
		SubFlows: subFlows,
	}
}
