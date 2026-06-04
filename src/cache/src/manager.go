package cache

import "fmt"

// CacheManager is the unified entry point for cache. It embeds ICache
// so all cache operations are directly available on the manager.
type CacheManager struct {
	ICache
}

// CacheManagerConfig mirrors config.CacheConfig, decoupled from the config package.
type CacheManagerConfig struct {
	Provider string
	Memory   *MemoryCacheConfig
	Redis    *RedisCacheConfig
}

// NewCacheManager creates the correct ICache implementation from config.
func NewCacheManager(cfg *CacheManagerConfig) (*CacheManager, error) {
	var c ICache
	var err error
	switch cfg.Provider {
	case "Redis":
		c, err = NewRedisCache(cfg.Redis)
		if err != nil {
			return nil, fmt.Errorf("cache: redis: %w", err)
		}
	default: // "Memory" or empty
		c = NewMemoryCache(cfg.Memory)
	}
	if c == nil {
		return nil, fmt.Errorf("cache: failed to create provider %q", cfg.Provider)
	}
	return &CacheManager{ICache: c}, nil
}
