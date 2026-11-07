package entities

// McpInfo is a DB-backed MCP (Model Context Protocol) server definition.
type McpInfo struct {
	BaseEntity

	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	Transport string            `json:"transport"`
	RPCURL    string            `json:"rpc_url,omitempty" db:"rpc_url"`
	Type      string            `json:"-" db:"-"` // deprecated runtime alias
	URL       string            `json:"-" db:"-"` // deprecated runtime alias
	Headers   map[string]string `json:"-" yaml:"-"`
	// HeaderRefs and EnvRefs are browser-safe runtime reference projections.
	// They are derived from persisted values and are never stored separately.
	HeaderRefs map[string]string `json:"header_refs,omitempty" db:"-"`
	Command    []string          `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"-" yaml:"-"`
	EnvRefs    map[string]string `json:"env_refs,omitempty" db:"-"`
	Labels     map[string]string `json:"labels,omitempty" yaml:"labels,omitempty" db:"-"`
}

func (m *McpInfo) NormalizeAliases() {
	if m.Transport == "" {
		m.Transport = m.Type
	}
	if m.Transport == "streamable-http" {
		m.Transport = "http"
	}
	if m.Type == "" {
		m.Type = m.Transport
	}
	if m.Type == "http" {
		m.Type = "streamable-http"
	}
	if m.RPCURL == "" {
		m.RPCURL = m.URL
	}
	if m.URL == "" {
		m.URL = m.RPCURL
	}
}
