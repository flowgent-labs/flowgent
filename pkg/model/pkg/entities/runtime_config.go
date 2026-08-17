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
	ScopeType            string              `json:"scope_type"`
	ScopeID              string              `json:"scope_id"`
	Environment          map[string]string   `json:"environment"`
	ConfiguredSecretKeys []string            `json:"configured_secret_keys"`
	SealedSecrets        *secretbox.Envelope `json:"-"`
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
