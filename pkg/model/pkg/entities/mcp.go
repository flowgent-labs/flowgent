package entities

// McpInfo is a DB-backed MCP (Model Context Protocol) server definition.
type McpInfo struct {
	BaseEntity

	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// HeaderRefs and EnvRefs are browser-safe runtime reference projections.
	// They are derived from persisted values and are never stored separately.
	HeaderRefs map[string]string `json:"header_refs,omitempty" db:"-"`
	Command    []string          `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	EnvRefs    map[string]string `json:"env_refs,omitempty" db:"-"`
	Labels     map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}
