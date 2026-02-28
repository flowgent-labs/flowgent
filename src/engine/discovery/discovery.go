// Package discovery provides pluggable service discovery for Flowgent
// distributed components (Controller sharding, JM HA leader election, TM peer list).
//
// Architecture (like Flink's HighAvailabilityServices):
//
//   IDiscoveryClient
//   ├── K8sDiscoveryClient     (label-selector → pod list)
//   ├── StaticDiscoveryClient  (env-var based, for dev/single-node)
//   └── Custom implementations (Consul, etcd, ZooKeeper in future)
//
// Usage:
//
//	JM HA:
//	  1. Discover peer JMs via label selector
//	  2. Elect one active JM (lowest pod name lexicographically)
//	  3. Active JM runs runPoller; standbys poll and take over if active gone
//
//	Controller sharding:
//	  1. Discover total controller pods
//	  2. Each pod computes shard(flow_id) = hash(flow_id) % totalPods
//	  3. Only process flows in own shard
//
//	TaskManager peers:
//	  1. Discover all TM pods
//	  2. Each TM claims execution plans independently (MQTT topic per TM)
package discovery

import (
	"context"
	"time"
)

// Peer represents a discovered instance of a component.
type Peer struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	PodIP     string    `json:"pod_ip"`
	Ready     bool      `json:"ready"`
	Since     time.Time `json:"since"`
}

// IDiscoveryClient abstracts service discovery for distributed components.
// Implementations: K8sDiscoveryClient (label selector), StaticDiscoveryClient (env vars).
type IDiscoveryClient interface {
	// DiscoverPeers returns all peers matching the given label selector.
	// Called periodically to handle scale-up/down events.
	DiscoverPeers(ctx context.Context, labelSelector string) ([]Peer, error)

	// Self returns this pod's identity within the peer group.
	Self() Peer

	// IsLeader returns true if this pod should be the active leader.
	// Used by JM HA — only the leader runs the runPoller.
	// Default strategy: lowest pod name in lexicographic order.
	IsLeader(ctx context.Context, labelSelector string) (bool, error)

	// WatchPeers returns a channel that emits the current peer list on changes.
	// Used for long-running components that need immediate reaction to scale events.
	WatchPeers(ctx context.Context, labelSelector string) (<-chan []Peer, error)
}

// LeaderElection implements simple leader election via lexicographic ordering.
// The peer with the alphabetically lowest name is elected leader.
// This is deterministic and requires no external coordination (no locks needed).
func LeaderElection(peers []Peer, self Peer) bool {
	if len(peers) == 0 {
		return false
	}
	leader := peers[0].Name
	for _, p := range peers[1:] {
		if p.Name < leader {
			leader = p.Name
		}
	}
	return leader == self.Name
}

// ShardIndex computes which shard a key belongs to.
// Returns the 0-based index of the pod that owns this key.
func ShardIndex(key string, totalPods int) int {
	if totalPods <= 1 {
		return 0
	}
	h := hashFnv64a(key)
	return int(h % uint64(totalPods))
}

func hashFnv64a(s string) uint64 {
	const offset = 14695981039346656037
	const prime = 1099511628211
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}
