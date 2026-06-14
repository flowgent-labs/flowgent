package model

import "time"

// MCPDef is a DB-backed MCP (Model Context Protocol) server definition.
type MCPDef struct {
	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	Type      string            `json:"type"`
	Command   []string          `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	TenantID  string            `json:"tenant_id"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
