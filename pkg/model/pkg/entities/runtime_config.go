package entities

import "github.com/flowgent-labs/flowgent/common/pkg/secretbox"

const (
	RuntimeConfigScopeNamespace  = "namespace"
	RuntimeConfigScopeAgentGraph = "graph"
)

// RuntimeConfiguration is the durable local configuration for one namespace
// or AgentGraph. Secret values are stored only in SealedSecrets; management clients
// receive ConfiguredSecretKeys instead of plaintext or the encrypted envelope.
type RuntimeConfiguration struct {
	BaseEntity
	Scope                string              `json:"scope"`
	AgentGraphID         string              `json:"agent_graph_id,omitempty"`
	AgentGraphName       string              `json:"agent_graph_name,omitempty" db:"-"`
	ScopeType            string              `json:"-" db:"-"`
	ScopeID              string              `json:"-" db:"-"`
	Environment          map[string]string   `json:"environment"`
	ConfiguredSecretKeys []string            `json:"configured_secret_keys"`
	SealedSecrets        *secretbox.Envelope `json:"-"`
}

func (r *RuntimeConfiguration) Normalize() {
	if r.Scope == "" {
		r.Scope = r.ScopeType
	}
	if r.ScopeType == "" {
		r.ScopeType = r.Scope
	}
	if r.AgentGraphName == "" && r.Scope == RuntimeConfigScopeAgentGraph {
		r.AgentGraphName = r.ScopeID
	}
	if r.ScopeID == "" && r.Scope == RuntimeConfigScopeAgentGraph {
		if r.AgentGraphName != "" {
			r.ScopeID = r.AgentGraphName
		} else {
			r.ScopeID = r.AgentGraphID
		}
	}
}

func (r *RuntimeConfiguration) AgentGraphKey() string {
	if r.AgentGraphName != "" {
		return r.AgentGraphName
	}
	if r.ScopeID != "" {
		return r.ScopeID
	}
	return r.AgentGraphID
}

// RuntimeConfigLayer is a redacted configuration layer returned to management
// clients. Secret values never cross this API boundary.
type RuntimeConfigLayer struct {
	Environment map[string]string `json:"environment"`
	SecretKeys  []string          `json:"secret_keys"`
}

// RuntimeConfigView makes inheritance explicit. Local AgentGraph values override
// inherited namespace values; Effective is the resulting redacted contract.
type RuntimeConfigView struct {
	Local     RuntimeConfigLayer `json:"local"`
	Inherited RuntimeConfigLayer `json:"inherited"`
	Effective RuntimeConfigLayer `json:"effective"`
}

// RuntimeConfigUpdate replaces the local environment map, merges write-only
// secret values, and removes named local secret overrides.
type RuntimeConfigUpdate struct {
	Environment     map[string]string `json:"environment"`
	Secrets         map[string]string `json:"secrets,omitempty"`
	ClearSecretKeys []string          `json:"clear_secret_keys,omitempty"`
}

// ResolvedRuntimeConfig is available only to the controller workload. It is
// materialized into a per-AgentGraph Kubernetes ConfigMap and Secret.
type ResolvedRuntimeConfig struct {
	Environment map[string]string `json:"environment"`
	Secrets     map[string]string `json:"secrets"`
}
