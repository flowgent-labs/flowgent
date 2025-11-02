package tools

import (
	"context"
	"testing"
	"time"
)

func TestGKETool_NewGKETool(t *testing.T) {
	config := GKEConfig{
		Cluster:   "test-cluster",
		Zone:      "us-central1-a",
		ProjectID: "test-project",
		Namespace: "default",
	}

	tool := NewGKETool(config)
	if tool == nil {
		t.Error("Expected non-nil GKETool")
	}
	if tool.config.Cluster != "test-cluster" {
		t.Errorf("Cluster mismatch")
	}
}

func TestGKETool_ConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config GKEConfig
		valid  bool
	}{
		{
			name: "valid config",
			config: GKEConfig{
				Cluster:   "cluster",
				Zone:      "zone",
				ProjectID: "project",
				Namespace: "ns",
			},
			valid: true,
		},
		{
			name: "missing cluster",
			config: GKEConfig{
				Zone:      "zone",
				ProjectID: "project",
				Namespace: "ns",
			},
			valid: false,
		},
		{
			name: "missing namespace",
			config: GKEConfig{
				Cluster:   "cluster",
				Zone:      "zone",
				ProjectID: "project",
			},
			valid: true, // namespace has default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewGKETool(tt.config)
			if tool == nil && tt.valid {
				t.Error("Expected non-nil tool for valid config")
			}
		})
	}
}

func TestGKETool_WaitAndVerify_Timeout(t *testing.T) {
	config := GKEConfig{
		Cluster:   "test-cluster",
		Zone:      "us-central1-a",
		ProjectID: "test-project",
		Namespace: "default",
	}

	tool := NewGKETool(config)
	ctx := context.Background()

	// This should timeout quickly since we don't have a real k8s cluster
	// We just verify the function handles errors gracefully
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := tool.WaitAndVerify(shortCtx, "test-service")
	if err == nil {
		t.Error("Expected error due to timeout or connection failure")
	}
}

func TestDeployStatus_Struct(t *testing.T) {
	status := DeployStatus{
		Success:    true,
		ServiceURL: "http://1.2.3.4:8080",
		Message:    "Deployment successful",
	}

	if !status.Success {
		t.Error("Expected Success to be true")
	}
	if status.ServiceURL != "http://1.2.3.4:8080" {
		t.Errorf("ServiceURL mismatch")
	}
}

func TestGKEConfig_Struct(t *testing.T) {
	config := GKEConfig{
		Cluster:   "prod-cluster",
		Zone:      "us-east1-b",
		ProjectID: "my-project",
		Namespace: "production",
	}

	if config.Cluster != "prod-cluster" {
		t.Errorf("Cluster mismatch")
	}
	if config.Zone != "us-east1-b" {
		t.Errorf("Zone mismatch")
	}
	if config.ProjectID != "my-project" {
		t.Errorf("ProjectID mismatch")
	}
	if config.Namespace != "production" {
		t.Errorf("Namespace mismatch")
	}
}
