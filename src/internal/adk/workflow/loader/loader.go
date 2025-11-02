package loader

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/cyberbot/cve-auto-fix/src/internal/adk/workflow"
)

// YAMLConfigLoader implements ConfigLoader for YAML files.
type YAMLConfigLoader struct{}

func (l *YAMLConfigLoader) Load(path string) (*workflow.WorkflowConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg workflow.WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func NewYAML() workflow.ConfigLoader {
	return &YAMLConfigLoader{}
}
