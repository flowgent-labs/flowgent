package handler

import (
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestValidateResourcePoolQuantities(t *testing.T) {
	valid := &entities.ResourcePoolInfo{
		Name: "critical", Replicas: 2, SlotsPerPod: 4,
		Resources: &model.SandboxResources{CPU: "500m", Memory: "2Gi"},
		SandboxReplicas: 1, SandboxSlotsPerPod: 2,
		SandboxResources: &model.SandboxResources{CPU: "1.5", Memory: "1024Mi"},
		NodeSelector: map[string]string{"workload.flowgent.io/tier": "critical"},
	}
	if err := validateResourcePool(valid); err != nil {
		t.Fatalf("valid resource pool rejected: %v", err)
	}

	invalid := *valid
	invalid.Resources = &model.SandboxResources{CPU: "not-a-quantity", Memory: "2Gi"}
	if err := validateResourcePool(&invalid); err == nil {
		t.Fatal("invalid CPU quantity accepted")
	}
}
