package cache

import (
	"testing"

	"github.com/flowgent-labs/flowgent/config/src"
)

func TestRedisCache_Standalone(t *testing.T) {
	rc, err := NewRedisCache(&config.RedisCacheConfig{
		Nodes:    []string{"redis://127.0.0.1:6379"},
		Username: "default",
		Retries:  3,
	})
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer rc.Close()
	if rc.NodeCount() != 1 {
		t.Errorf("expected 1 node, got %d", rc.NodeCount())
	}
	if rc.IsCluster() {
		t.Error("single node should not be cluster")
	}
}

func TestRedisCache_ClusterDetection(t *testing.T) {
	rc, err := NewRedisCache(&config.RedisCacheConfig{
		Nodes: []string{"redis://10.0.0.1:6379", "redis://10.0.0.2:6379", "redis://10.0.0.3:6379"},
	})
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer rc.Close()
	if rc.NodeCount() != 3 {
		t.Errorf("expected 3 nodes, got %d", rc.NodeCount())
	}
	if !rc.IsCluster() {
		t.Error("3 nodes should be cluster")
	}
}
