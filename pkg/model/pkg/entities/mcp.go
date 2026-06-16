package entities

// McpInfo is a DB-backed MCP (Model Context Protocol) server definition.
type McpInfo struct {
	BaseEntity

	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Type    string            `json:"type"`
	Command []string          `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}
