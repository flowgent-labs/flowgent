package cache

import (
	"context"
	"sync"
	"time"

	
)

type entry struct {
	value   []byte
	expires time.Time
}

// MemoryCache implements ICache with an in-memory LRU map.
type MemoryCache struct {
	mu       sync.RWMutex
	store    map[string]*entry
	capacity int
	maxCap   int
	stopCh   chan struct{}
}

// NewMemoryCache creates an in-memory cache from MemoryCacheConfig.
func NewMemoryCache(cfg *MemoryCacheConfig) *MemoryCache {
	initCap := cfg.InitialCapacity
	if initCap <= 0 {
		initCap = 32
	}
	maxCap := cfg.MaxCapacity
	if maxCap <= 0 {
		maxCap = 65535
	}
	c := &MemoryCache{
		store:    make(map[string]*entry, initCap),
		capacity: initCap,
		maxCap:   maxCap,
		stopCh:   make(chan struct{}),
	}
	// Background eviction goroutine
	go c.evictLoop()
	return c
}

func (c *MemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	e, ok := c.store[key]
	c.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	if time.Now().After(e.expires) {
		c.mu.Lock()
		delete(c.store, key)
		c.mu.Unlock()
		return nil, nil
	}
	return e.value, nil
}

func (c *MemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.store) >= c.maxCap {
		// LRU: evict first entry
		for k := range c.store {
			delete(c.store, k)
			break
		}
	}
	c.store[key] = &entry{value: value, expires: time.Now().Add(ttl)}
	return nil
}

func (c *MemoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	delete(c.store, key)
	c.mu.Unlock()
	return nil
}

func (c *MemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	c.mu.RLock()
	e, ok := c.store[key]
	c.mu.RUnlock()
	if !ok {
		return false, nil
	}
	return time.Now().Before(e.expires), nil
}

func (c *MemoryCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	c.store = make(map[string]*entry)
	c.mu.Unlock()
	return nil
}

func (c *MemoryCache) Close() error {
	close(c.stopCh)
	return nil
}

func (c *MemoryCache) evictLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			c.mu.Lock()
			for k, e := range c.store {
				if now.After(e.expires) {
					delete(c.store, k)
				}
			}
			c.mu.Unlock()
		}
	}
}
