package discovery

import (
	"context"
	"os"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// K8sDiscoveryClient discovers peer pods via K8s API label selectors.
// Like Flink's KubernetesHighAvailabilityServices.
type K8sDiscoveryClient struct {
	clientset *kubernetes.Clientset
	namespace string
	self      Peer
	mu        sync.RWMutex
	watchers  map[string]chan []Peer
}

// NewK8sDiscoveryClient creates a discovery client using in-cluster K8s config.
func NewK8sDiscoveryClient() (*K8sDiscoveryClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	podName := os.Getenv("POD_NAME")
	podIP := os.Getenv("POD_IP")

	return &K8sDiscoveryClient{
		clientset: clientset,
		namespace: namespace,
		self: Peer{
			Name:      podName,
			Namespace: namespace,
			PodIP:     podIP,
			Ready:     true,
			Since:     time.Now(),
		},
		watchers: make(map[string]chan []Peer),
	}, nil
}

// DiscoverPeers lists pods matching labelSelector in the client's namespace.
func (c *K8sDiscoveryClient) DiscoverPeers(ctx context.Context, labelSelector string) ([]Peer, error) {
	pods, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}

	var peers []Peer
	for _, pod := range pods.Items {
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
				break
			}
		}
		peers = append(peers, Peer{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			PodIP:     pod.Status.PodIP,
			Ready:     ready,
			Since:     pod.CreationTimestamp.Time,
		})
	}
	return peers, nil
}

// Self returns this pod's identity.
func (c *K8sDiscoveryClient) Self() Peer {
	return c.self
}

// IsLeader returns true if this pod is the JM leader (lowest-name pod wins).
func (c *K8sDiscoveryClient) IsLeader(ctx context.Context, labelSelector string) (bool, error) {
	peers, err := c.DiscoverPeers(ctx, labelSelector)
	if err != nil {
		return false, err
	}
	// Filter to only ready peers for leader election
	var ready []Peer
	for _, p := range peers {
		if p.Ready {
			ready = append(ready, p)
		}
	}
	return LeaderElection(ready, c.self), nil
}

// WatchPeers polls K8s and emits peer lists on changes.
func (c *K8sDiscoveryClient) WatchPeers(ctx context.Context, labelSelector string) (<-chan []Peer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ch, ok := c.watchers[labelSelector]; ok {
		return ch, nil
	}

	ch := make(chan []Peer, 10)
	c.watchers[labelSelector] = ch

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var lastHash uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				peers, err := c.DiscoverPeers(ctx, labelSelector)
				if err != nil {
					continue
				}
				// Only emit on changes
				h := hashPeers(peers)
				if h != lastHash {
					lastHash = h
					select {
					case ch <- peers:
					default:
					}
				}
			}
		}
	}()

	return ch, nil
}

func hashPeers(peers []Peer) uint64 {
	if len(peers) == 0 {
		return 0
	}
	h := uint64(14695981039346656037)
	for _, p := range peers {
		h ^= hashFnv64a(p.Name)
		h *= 1099511628211
	}
	return h
}
