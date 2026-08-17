package entities

import "github.com/flowgent-labs/flowgent/model/pkg"

// ResourcePoolInfo is a namespace-scoped, explicitly provisioned worker
// capacity boundary. Flows bind to a pool by Name; runs snapshot that binding
// so an in-flight run never moves when its Flow definition is edited.
type ResourcePoolInfo struct {
	BaseEntity

	Name               string                  `json:"name" yaml:"name"`
	Replicas           int                     `json:"replicas" yaml:"replicas"`
	SlotsPerPod        int                     `json:"slots_per_pod" yaml:"slots_per_pod"`
	Resources          *model.SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	SandboxReplicas    int                     `json:"sandbox_replicas" yaml:"sandbox_replicas"`
	SandboxSlotsPerPod int                     `json:"sandbox_slots_per_pod" yaml:"sandbox_slots_per_pod"`
	SandboxResources   *model.SandboxResources `json:"sandbox_resources,omitempty" yaml:"sandbox_resources,omitempty"`
	PriorityClassName  string                  `json:"priority_class_name,omitempty" yaml:"priority_class_name,omitempty"`
	NodeSelector       map[string]string       `json:"node_selector,omitempty" yaml:"node_selector,omitempty"`
}

// NormalizeResourcePool applies stable operational defaults. It is shared by
// API validation and runtime clients to keep the persisted contract explicit.
func NormalizeResourcePool(pool *ResourcePoolInfo) {
	if pool.Replicas <= 0 {
		pool.Replicas = 1
	}
	if pool.SlotsPerPod <= 0 {
		pool.SlotsPerPod = 4
	}
	if pool.SandboxReplicas < 0 {
		pool.SandboxReplicas = 0
	}
	if pool.SandboxSlotsPerPod <= 0 {
		pool.SandboxSlotsPerPod = 4
	}
}
