package lock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisLock implements DistributedLock using Redis SET NX + Lua scripts.
type RedisLock struct {
	client redis.Cmdable
	mu     sync.RWMutex
	locks  map[string]string // key → lock value
}

// NewRedisLock creates a Redis-backed distributed lock.
func NewRedisLock(client any) (*RedisLock, error) {
	cmdable, ok := client.(redis.Cmdable)
	if !ok {
		return nil, fmt.Errorf("client must implement redis.Cmdable")
	}
	return &RedisLock{
		client: cmdable,
		locks:  make(map[string]string),
	}, nil
}

func (r *RedisLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	value := uuid.New().String()
	lockKey := "dlock:" + key

	ok, err := r.client.SetNX(ctx, lockKey, value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire lock %s: %w", key, err)
	}
	if ok {
		r.mu.Lock()
		r.locks[key] = value
		r.mu.Unlock()
	}
	return ok, nil
}

func (r *RedisLock) Release(ctx context.Context, key string) error {
	lockKey := "dlock:" + key

	r.mu.RLock()
	value, ok := r.locks[key]
	r.mu.RUnlock()
	if !ok {
		return nil
	}

	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`
	result, err := r.client.Eval(ctx, script, []string{lockKey}, value).Result()
	if err != nil {
		return fmt.Errorf("release lock %s: %w", key, err)
	}
	if result.(int64) > 0 {
		r.mu.Lock()
		delete(r.locks, key)
		r.mu.Unlock()
	}
	return nil
}

func (r *RedisLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	lockKey := "dlock:" + key

	r.mu.RLock()
	value, ok := r.locks[key]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("lock %s not held", key)
	}

	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`
	result, err := r.client.Eval(ctx, script, []string{lockKey}, value, int(ttl.Milliseconds())).Result()
	if err != nil {
		return fmt.Errorf("extend lock %s: %w", key, err)
	}
	if result.(int64) == 0 {
		return fmt.Errorf("lock %s lost", key)
	}
	return nil
}
