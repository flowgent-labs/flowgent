package cache

import (
	"context"
	"crypto/tls"
	"strings"
	"time"

	
	"github.com/redis/go-redis/v9"
)

// RedisCache implements ICache using go-redis, with auto-detection of
// standalone vs cluster vs sentinel mode based on node configuration.
type RedisCache struct {
	client  redis.UniversalClient
	cluster bool
	nodes   int
}

// NewRedisCache creates a Redis cache from config.
//
// Auto-detection rules:
//   - Single node             → standalone client
//   - Multiple nodes          → cluster client (auto-discover topology)
//   - Node URL contains "sentinel://" → failover client
func NewRedisCache(cfg *RedisCacheConfig) (*RedisCache, error) {
	addrs, username, password := parseRedisAddrs(cfg)
	if len(addrs) == 0 {
		addrs = []string{"127.0.0.1:6379"}
	}

	var client redis.UniversalClient
	isSentinel := strings.HasPrefix(cfg.Nodes[0], "sentinel://")

	switch {
	case isSentinel:
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:      "mymaster",
			SentinelAddrs:   addrs,
			Username:        username,
			Password:        password,
			DialTimeout:     msToDuration(cfg.ConnectionTimeout),
			ReadTimeout:     msToDuration(cfg.ResponseTimeout),
			MaxRetries:      cfg.Retries,
			MinRetryBackoff: msToDuration(cfg.MinRetryWait),
			MaxRetryBackoff: msToDuration(cfg.MaxRetryWait),
		})

	case len(addrs) > 1:
		// Cluster mode: auto-discover all nodes in the cluster
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:           addrs,
			Username:        username,
			Password:        password,
			DialTimeout:     msToDuration(cfg.ConnectionTimeout),
			ReadTimeout:     msToDuration(cfg.ResponseTimeout),
			MaxRetries:      cfg.Retries,
			MinRetryBackoff: msToDuration(cfg.MinRetryWait),
			MaxRetryBackoff: msToDuration(cfg.MaxRetryWait),
			ReadOnly:        cfg.ReadFromReplica,
		})

	default:
		// Standalone mode
		client = redis.NewClient(&redis.Options{
			Addr:            addrs[0],
			Username:        username,
			Password:        password,
			DialTimeout:     msToDuration(cfg.ConnectionTimeout),
			ReadTimeout:     msToDuration(cfg.ResponseTimeout),
			MaxRetries:      cfg.Retries,
			MinRetryBackoff: msToDuration(cfg.MinRetryWait),
			MaxRetryBackoff: msToDuration(cfg.MaxRetryWait),
			TLSConfig:       tlsConfig(cfg.UseSSL),
		})
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisCache{
		client:  client,
		cluster: isSentinel || len(addrs) > 1,
		nodes:   len(addrs),
	}, nil
}

func parseRedisAddrs(cfg *RedisCacheConfig) ([]string, string, string) {
	var addrs []string
	for _, n := range cfg.Nodes {
		n = strings.TrimPrefix(n, "redis://")
		n = strings.TrimPrefix(n, "sentinel://")
		n = strings.TrimSuffix(n, "/")
		if n != "" {
			addrs = append(addrs, n)
		}
	}
	return addrs, cfg.Username, cfg.Password
}

func msToDuration(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func tlsConfig(useSSL bool) *tls.Config {
	if !useSSL {
		return nil
	}
	return &tls.Config{InsecureSkipVerify: true}
}

func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return val, err
}

func (c *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, key).Result()
	return n > 0, err
}

func (c *RedisCache) Clear(ctx context.Context) error {
	return c.client.FlushDB(ctx).Err()
}

// IsCluster returns true if operating in cluster mode (>1 node).
func (c *RedisCache) IsCluster() bool { return c.cluster }

// NodeCount returns the number of configured nodes.
func (c *RedisCache) NodeCount() int { return c.nodes }

func (c *RedisCache) Close() error {
	return c.client.Close()
}
