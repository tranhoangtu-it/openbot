package agent

import (
	"context"
	"sync"
	"time"
)

// RateLimiter is a token bucket for throttling LLM API calls.
type RateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func NewRateLimiter(maxBurst int, ratePerMinute float64) *RateLimiter {
	if maxBurst <= 0 {
		maxBurst = 10
	}
	if ratePerMinute <= 0 {
		ratePerMinute = 30 // 30 requests per minute default
	}
	return &RateLimiter{
		tokens:   float64(maxBurst),
		max:      float64(maxBurst),
		rate:     ratePerMinute / 60.0, // Convert to per-second
		lastTime: time.Now(),
	}
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		rl.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(rl.lastTime).Seconds()
		rl.tokens += elapsed * rl.rate
		if rl.tokens > rl.max {
			rl.tokens = rl.max
		}
		rl.lastTime = now

		if rl.tokens >= 1.0 {
			rl.tokens -= 1.0
			rl.mu.Unlock()
			return nil
		}

		waitSec := (1.0 - rl.tokens) / rl.rate
		rl.mu.Unlock()

		timer := time.NewTimer(time.Duration(waitSec * float64(time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
