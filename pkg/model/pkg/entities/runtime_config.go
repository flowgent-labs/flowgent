package entities

import "github.com/flowgent-labs/flowgent/common/pkg/secretbox"

const (
	RuntimeConfigScopeNamespace = "namespace"
	RuntimeConfigScopeFlow      = "flow"
)

// RuntimeConfiguration is the durable local configuration for one namespace
// or Flow. Secret values are stored only in SealedSecrets; management clients
// receive ConfiguredSecretKeys instead of plaintext or the encrypted envelope.
type RuntimeConfiguration struct {
	BaseEntity
	Scope                string              `json:"scope"`
	FlowID               string              `json:"flow_id,omitempty"`
	FlowName             string              `json:"flow_name,omitempty" db:"-"`
	ScopeType            string              `json:"-" db:"-"`
	ScopeID              string              `json:"-" db:"-"`
	Environment          map[string]string   `json:"environment"`
	ConfiguredSecretKeys []string            `json:"configured_secret_keys"`
	SealedSecrets        *secretbox.Envelope `json:"-"`
}

func (r *RuntimeConfiguration) NormalizeAliases() {
	if r.Scope == "" {
		r.Scope = r.ScopeType
	}
	if r.ScopeType == "" {
		r.ScopeType = r.Scope
	}
	if r.FlowName == "" && r.Scope == RuntimeConfigScopeFlow {
		r.FlowName = r.ScopeID
	}
	if r.ScopeID == "" && r.Scope == RuntimeConfigScopeFlow {
		if r.FlowName != "" {
			r.ScopeID = r.FlowName
		} else {
			r.ScopeID = r.FlowID
		}
	}
}

func (r *RuntimeConfiguration) FlowKey() string {
	if r.FlowName != "" {
		return r.FlowName
	}
	if r.ScopeID != "" {
		return r.ScopeID
	}
	return r.FlowID
}

// RuntimeConfigLayer is a redacted configuration layer returned to management
// clients. Secret values never cross this API boundary.
type RuntimeConfigLayer struct {
	Environment map[string]string `json:"environment"`
	SecretKeys  []string          `json:"secret_keys"`
}

// RuntimeConfigView makes inheritance explicit. Local Flow values override
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
// materialized into a per-Flow Kubernetes ConfigMap and Secret.
type ResolvedRuntimeConfig struct {
	Environment map[string]string `json:"environment"`
	Secrets     map[string]string `json:"secrets"`
}
