package discovery

import (
	"context"
	"fmt"
	"os"
	"time"
)

// StaticDiscoveryClient uses configuration for pod discovery.
// Used in dev/single-node/CI where K8s API is not available.
type StaticDiscoveryClient struct {
	self      Peer
	totalPods int
	podIndex  int
	peers     []Peer
}

// NewStaticDiscoveryClient creates a discovery client from RuntimeConfig values.
// podTotal and podIndex come from cfg.Runtime.PodTotal / cfg.Runtime.PodIndex.
func NewStaticDiscoveryClient(podTotal, podIndex int) *StaticDiscoveryClient {
	total := podTotal
	if total <= 0 {
		total = 1
	}
	index := podIndex
	if index < 0 || index >= total {
		index = 0
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
