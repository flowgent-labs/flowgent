package entities

// McpInfo is a DB-backed MCP (Model Context Protocol) server definition.
type McpInfo struct {
	BaseEntity

	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command []string          `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Labels  map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}
