package tools

import "context"

// Tool represents a single callable tool.
type Tool struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Params  map[string]any `yaml:"params"`
	Execute func(ctx context.Context, params map[string]any) (string, error)
}
