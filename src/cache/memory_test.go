package cache

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
)

func TestMemoryCache_SetGet(t *testing.T) {
	c := NewMemoryCache(&model.MemoryCacheConfig{
		InitialCapacity: 10, MaxCapacity: 100, TTL: 60, EvictionPolicy: "LRU",
	})
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "key", []byte("val"), time.Minute)
	got, _ := c.Get(ctx, "key")
	if string(got) != "val" {
		t.Errorf("got %q, want 'val'", got)
	}
}

func TestMemoryCache_Exists(t *testing.T) {
	c := NewMemoryCache(&model.MemoryCacheConfig{InitialCapacity: 10})
	defer c.Close()
	ctx := context.Background()

	ok, _ := c.Exists(ctx, "nope")
	if ok {
		t.Error("should not exist")
	}
	c.Set(ctx, "k", []byte("v"), time.Minute)
	ok, _ = c.Exists(ctx, "k")
	if !ok {
		t.Error("should exist")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	c := NewMemoryCache(&model.MemoryCacheConfig{InitialCapacity: 10})
	defer c.Close()
	ctx := context.Background()
	c.Set(ctx, "k", []byte("v"), time.Minute)
	c.Delete(ctx, "k")
	got, _ := c.Get(ctx, "k")
	if got != nil {
		t.Error("should be nil after delete")
	}
}

func TestMemoryCache_Clear(t *testing.T) {
	c := NewMemoryCache(&model.MemoryCacheConfig{InitialCapacity: 10})
	defer c.Close()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		c.Set(ctx, string(rune('a'+i)), []byte{byte(i)}, time.Minute)
	}
	c.Clear(ctx)
	for i := 0; i < 5; i++ {
		got, _ := c.Get(ctx, string(rune('a'+i)))
		if got != nil {
			t.Errorf("%c should be nil", rune('a'+i))
		}
	}
}

func TestMemoryCache_TTL(t *testing.T) {
	c := NewMemoryCache(&model.MemoryCacheConfig{InitialCapacity: 10})
	defer c.Close()
	ctx := context.Background()
	c.Set(ctx, "k", []byte("v"), 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	got, _ := c.Get(ctx, "k")
	if got != nil {
		t.Error("should expire")
	}
}
