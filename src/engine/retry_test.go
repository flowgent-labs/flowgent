package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWithBackoff_Success(t *testing.T) {
	ctx := context.Background()
	calls := 0
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 3, Initial: 1 * time.Millisecond, Factor: 1, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryWithBackoff_RetriesThenSuccess(t *testing.T) {
	ctx := context.Background()
	calls := 0
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 3, Initial: 1 * time.Millisecond, Factor: 1, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		if calls <= 2 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWithBackoff_Exhausted(t *testing.T) {
	ctx := context.Background()
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 2, Initial: 1 * time.Millisecond, Factor: 1, MaxDelay: 10 * time.Millisecond}, func() error {
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRetryWithBackoff_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 3, Initial: time.Second, Factor: 1, MaxDelay: time.Second}, func() error {
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestModelRetry_Nil(t *testing.T) {
	rp := ModelRetry(nil)
	if rp.Max != 3 {
		t.Errorf("default max should be 3, got %d", rp.Max)
	}
	if rp.Initial != time.Second {
		t.Errorf("default initial should be 1s, got %v", rp.Initial)
	}
}
