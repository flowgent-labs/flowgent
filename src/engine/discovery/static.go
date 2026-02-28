package discovery

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"
)

// StaticDiscoveryClient uses environment variables for pod discovery.
// Used in dev/single-node/CI where K8s API is not available.
//
// Env vars:
//
//	FLOWGENT_CONTROLLER_INDEX  — this pod's index (0-based)
//	FLOWGENT_CONTROLLER_TOTAL  — total pod count
//	POD_NAME                   — pod name (defaults to "controller-<index>")
type StaticDiscoveryClient struct {
	self       Peer
	totalPods  int
	podIndex   int
	peers      []Peer
}

// NewStaticDiscoveryClient creates a discovery client from env vars.
func NewStaticDiscoveryClient() *StaticDiscoveryClient {
	total := 1
	if v := os.Getenv("FLOWGENT_CONTROLLER_TOTAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			total = n
		}
	}
	index := 0
	if v := os.Getenv("FLOWGENT_CONTROLLER_INDEX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < total {
			index = n
		}
	}
	name := os.Getenv("POD_NAME")
	if name == "" {
		name = fmt.Sprintf("controller-%d", index)
	}

	var peers []Peer
	for i := 0; i < total; i++ {
		peers = append(peers, Peer{
			Name:  fmt.Sprintf("controller-%d", i),
			Ready: true,
			Since: time.Now(),
		})
	}

	return &StaticDiscoveryClient{
		self: Peer{
			Name:  name,
			Ready: true,
			Since: time.Now(),
		},
		totalPods: total,
		podIndex:  index,
		peers:     peers,
	}
}

func (c *StaticDiscoveryClient) DiscoverPeers(ctx context.Context, labelSelector string) ([]Peer, error) {
	return c.peers, nil
}

func (c *StaticDiscoveryClient) Self() Peer {
	return c.self
}

func (c *StaticDiscoveryClient) IsLeader(ctx context.Context, labelSelector string) (bool, error) {
	return LeaderElection(c.peers, c.self), nil
}

func (c *StaticDiscoveryClient) WatchPeers(ctx context.Context, labelSelector string) (<-chan []Peer, error) {
	ch := make(chan []Peer, 1)
	ch <- c.peers
	return ch, nil
}
