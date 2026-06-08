// Package model defines the shared domain types for the Flowgent engine.
//
// File: sandbox.go — Sandbox security policy and resources consumed by sandbox worker.
//
//	SandboxPolicy, NetworkPolicy, SandboxPolicyOverride, SandboxResources,
//	SandboxTrigger, SandboxDeploymentConfig.
package model

// ─── Sandbox resources ───────────────────────────────────────

// SandboxResources defines CPU/memory limits for a sandbox execution or pod.
type SandboxResources struct {
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`       // e.g. "500m"
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"` // e.g. "256Mi"
}

// ─── Sandbox trigger (TM → Sandbox) ──────────────────────────

// SandboxTrigger is the message published from SandboxExecutor (TM side) to
// SandboxRunner pods via MQTT. Defined in model so both sides share the contract.
type SandboxTrigger struct {
	TenantID      string            `json:"tenant_id,omitempty"`
	FlowID        string            `json:"flow_id"`
	RunID         string            `json:"run_id"`
	PlanID        string            `json:"plan_id"`
	ScriptPath    string            `json:"script_path"`
	Runtime       string            `json:"runtime"`
	Timeout       string            `json:"timeout"`
	Resources     *SandboxResources `json:"resources,omitempty"`
	NetworkPolicy *NetworkPolicy    `json:"network_policy,omitempty"`
	Workspace     string            `json:"workspace,omitempty"`
	SpanID        string            `json:"span_id"`
}

// ─── Sandbox deployment config ───────────────────────────────

// SandboxDeploymentConfig defines K8s deployment settings for sandbox pods.
// When Enabled=false (default), sandbox runs inline in TM (standalone/all-in-one).
// When Enabled=true, JM's K8sRM manages a separate sandbox Deployment.
type SandboxDeploymentConfig struct {
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Image       string            `json:"image,omitempty" yaml:"image,omitempty"`
	MinReplicas int               `json:"min_replicas,omitempty" yaml:"min_replicas,omitempty"`
	MaxReplicas int               `json:"max_replicas,omitempty" yaml:"max_replicas,omitempty"`
	Resources   *SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	SlotsPerPod int               `json:"slots_per_pod,omitempty" yaml:"slots_per_pod,omitempty"`
}

// ─── Sandbox security policy ─────────────────────────────────

// SandboxPolicy defines global static security policy for sandbox execution.
// Configured in flowgent.yaml under orchestration.sandbox.policy.
// Can be overridden per-flow (AgentFlowSpec.SandboxPolicy) and per-node (Node.NetworkPolicy).
type SandboxPolicy struct {
	// Network defines egress network restrictions.
	Network NetworkPolicy `json:"network" yaml:"network"`

	// AllowedRuntimes restricts which runtimes can be used. Empty = allow all.
	AllowedRuntimes []string `json:"allowed_runtimes,omitempty" yaml:"allowed_runtimes,omitempty"`

	// BannedCommands is a list of forbidden commands/patterns. Scripts containing
	// any of these patterns are rejected before execution.
	BannedCommands []string `json:"banned_commands,omitempty" yaml:"banned_commands,omitempty"`

	// DefaultTimeout is the fallback when no node-level timeout is set.
	DefaultTimeout string `json:"default_timeout" yaml:"default_timeout"` // e.g. "120s"

	// MaxTimeout is the absolute cap regardless of node-level setting.
	MaxTimeout string `json:"max_timeout" yaml:"max_timeout"` // e.g. "600s"

	// DefaultResources is the fallback when no node-level resources are set.
	DefaultResources *SandboxResources `json:"default_resources,omitempty" yaml:"default_resources,omitempty"`

	// MaxResources caps per-node resource requests.
	MaxResources *SandboxResources `json:"max_resources,omitempty" yaml:"max_resources,omitempty"`
}

// NetworkPolicy defines egress network restrictions for sandbox execution.
type NetworkPolicy struct {
	// Mode: "none" (no egress), "allowlist" (only allowed targets), "denylist" (block listed targets).
	// Default: "none".
	Mode string `json:"mode" yaml:"mode"`

	// Allowed is the explicit allowlist of host:port targets. Only effective in "allowlist" mode.
	Allowed []string `json:"allowed,omitempty" yaml:"allowed,omitempty"`

	// Denied is the explicit denylist of host:port targets. Only effective in "denylist" mode.
	Denied []string `json:"denied,omitempty" yaml:"denied,omitempty"`
}

// SandboxPolicyOverride allows per-flow or per-node policy overrides.
// nil fields inherit from the parent policy (global → flow → node).
type SandboxPolicyOverride struct {
	Network   *NetworkPolicy    `json:"network,omitempty" yaml:"network,omitempty"`
	Timeout   string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Resources *SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// EffectiveNetworkPolicy resolves the effective network policy from global → flow → node.
func EffectiveNetworkPolicy(global *NetworkPolicy, flowOverride, nodeOverride *NetworkPolicy) *NetworkPolicy {
	if nodeOverride != nil {
		return nodeOverride
	}
	if flowOverride != nil {
		return flowOverride
	}
	if global != nil {
		return global
	}
	return &NetworkPolicy{Mode: "none"}
}

// EffectiveTimeout resolves the effective timeout from global defaults → flow → node.
func EffectiveTimeout(globalDefault, globalMax, flowTimeout, nodeTimeout string) string {
	if nodeTimeout != "" {
		return nodeTimeout
	}
	if flowTimeout != "" {
		return flowTimeout
	}
	if globalDefault != "" {
		return globalDefault
	}
	return "120s"
}

// EffectiveResources resolves the effective resources from global defaults → flow → node.
func EffectiveResources(globalDefault, globalMax, flowRes, nodeRes *SandboxResources) *SandboxResources {
	if nodeRes != nil {
		return nodeRes
	}
	if flowRes != nil {
		return flowRes
	}
	if globalDefault != nil {
		return globalDefault
	}
	return &SandboxResources{CPU: "500m", Memory: "256Mi"}
}
