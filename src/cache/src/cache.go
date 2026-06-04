package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/config/src/config"
)

// ICache is the unified caching interface for flowgent.
type ICache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Clear(ctx context.Context) error
	Close() error
}
// CacheManager is the unified entry point for cache. It embeds ICache
// so all cache operations are directly available on the manager.
type CacheManager struct {
	ICache
}

// NewCacheManager creates the correct ICache implementation from FlowgentConfig.
func NewCacheManager(cfg *config.FlowgentConfig) (*CacheManager, error) {
	var c ICache
	var err error
	switch cfg.Cache.Provider {
	case "Redis":
		c, err = NewRedisCache(&RedisCacheConfig{
			Nodes:             cfg.Cache.Redis.Nodes,
			Username:          cfg.Cache.Redis.Username,
			Password:          cfg.Cache.Redis.Password,
			ConnectionTimeout: cfg.Cache.Redis.ConnectionTimeout,
			ResponseTimeout:   cfg.Cache.Redis.ResponseTimeout,
			Retries:           cfg.Cache.Redis.Retries,
			MaxRetryWait:      cfg.Cache.Redis.MaxRetryWait,
			MinRetryWait:      cfg.Cache.Redis.MinRetryWait,
			ReadFromReplica:   cfg.Cache.Redis.ReadFromReplica,
			UseSSL:            cfg.Cache.Redis.UseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("cache: redis: %w", err)
		}
	default: // "Memory" or empty
		c = NewMemoryCache(&MemoryCacheConfig{
			InitialCapacity: cfg.Cache.Memory.InitialCapacity,
			MaxCapacity:     cfg.Cache.Memory.MaxCapacity,
			TTL:             cfg.Cache.Memory.TTL,
			EvictionPolicy:  cfg.Cache.Memory.EvictionPolicy,
		})
	}
	if c == nil {
		return nil, fmt.Errorf("cache: failed to create provider %q", cfg.Cache.Provider)
	}
	return &CacheManager{ICache: c}, nil
}