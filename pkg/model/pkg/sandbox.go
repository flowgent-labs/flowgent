package model

// SandboxResources defines CPU/memory limits for a sandbox execution or pod.
type SandboxResources struct {
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
}

// SandboxTrigger is the message published from SandboxExecutor (TM side) to
// SandboxRunner pods via MQTT.
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
	Env           map[string]string `json:"env,omitempty"`
}

// SandboxDeploymentConfig defines K8s deployment settings for sandbox pods.
type SandboxDeploymentConfig struct {
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Image       string            `json:"image,omitempty" yaml:"image,omitempty"`
	MinReplicas int               `json:"min_replicas,omitempty" yaml:"min_replicas,omitempty"`
	MaxReplicas int               `json:"max_replicas,omitempty" yaml:"max_replicas,omitempty"`
	Resources   *SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	SlotsPerPod int               `json:"slots_per_pod,omitempty" yaml:"slots_per_pod,omitempty"`
}

// SandboxPolicy defines global static security policy for sandbox execution.
type SandboxPolicy struct {
	Network          NetworkPolicy     `json:"network" yaml:"network"`
	AllowedRuntimes  []string          `json:"allowed_runtimes,omitempty" yaml:"allowed_runtimes,omitempty"`
	BannedCommands   []string          `json:"banned_commands,omitempty" yaml:"banned_commands,omitempty"`
	DefaultTimeout   string            `json:"default_timeout" yaml:"default_timeout"`
	MaxTimeout       string            `json:"max_timeout" yaml:"max_timeout"`
	DefaultResources *SandboxResources `json:"default_resources,omitempty" yaml:"default_resources,omitempty"`
	MaxResources     *SandboxResources `json:"max_resources,omitempty" yaml:"max_resources,omitempty"`
}

// NetworkPolicy defines egress network restrictions for sandbox execution.
type NetworkPolicy struct {
	Mode    string   `json:"mode" yaml:"mode"`
	Allowed []string `json:"allowed,omitempty" yaml:"allowed,omitempty"`
	Denied  []string `json:"denied,omitempty" yaml:"denied,omitempty"`
}

// SandboxPolicyOverride allows per-flow or per-node policy overrides.
type SandboxPolicyOverride struct {
	Network   *NetworkPolicy    `json:"network,omitempty" yaml:"network,omitempty"`
	Timeout   string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Resources *SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
}

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
