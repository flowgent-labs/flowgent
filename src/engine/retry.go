package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
)

type RetryPolicy struct {
	Max      int
	Initial  time.Duration
	MaxDelay time.Duration
	Factor   float64
}

// ModelRetry converts model.RetryPolicy to engine RetryPolicy.
func ModelRetry(r *model.RetryPolicy) RetryPolicy {
	if r == nil {
		return RetryPolicy{Max: 3, Initial: time.Second, MaxDelay: 30 * time.Second, Factor: 2.0}
	}
	rp := RetryPolicy{Max: r.Max, Initial: r.Initial, MaxDelay: r.MaxDelay, Factor: r.Factor}
	if rp.Max <= 0 {
		rp.Max = 3
	}
	if rp.Initial <= 0 {
		rp.Initial = time.Second
	}
	if rp.MaxDelay <= 0 {
		rp.MaxDelay = 30 * time.Second
	}
	if rp.Factor <= 0 {
		rp.Factor = 2.0
	}
	return rp
}

func RetryWithBackoff(ctx context.Context, policy RetryPolicy, fn func() error) error {
	var err error
	delay := policy.Initial
	for i := 0; i <= policy.Max; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if i > 0 {
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * policy.Factor)
			if delay > policy.MaxDelay {
				delay = policy.MaxDelay
			}
		}
		err = fn()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", policy.Max, err)
}
