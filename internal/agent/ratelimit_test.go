package agent

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_ImmediateBurst(t *testing.T) {
	rl := NewRateLimiter(5, 60.0)

	// Should be able to consume 5 tokens immediately (burst)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("burst token %d failed: %v", i, err)
		}
	}
}

func TestRateLimiter_WaitsAfterBurst(t *testing.T) {
	rl := NewRateLimiter(1, 600.0) // 1 burst, 10/sec refill

	ctx := context.Background()
	// Consume the single burst token
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	// Next wait should block briefly
	start := time.Now()
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	elapsed := time.Since(start)

	// Should have waited ~100ms (1 token / 10 per sec)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected some wait time, got %v", elapsed)
	}
}

func TestRateLimiter_CancelledContext(t *testing.T) {
	rl := NewRateLimiter(1, 1.0) // 1 burst, very slow refill

	ctx, cancel := context.WithCancel(context.Background())

	// Drain the burst
	if err := rl.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// Cancel context before next wait completes
	cancel()
	err := rl.Wait(ctx)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestRateLimiter_DefaultValues(t *testing.T) {
	// Zero/negative values should use defaults
	rl := NewRateLimiter(0, 0)
	if rl.max != 10 {
		t.Fatalf("expected default max=10, got %v", rl.max)
	}
	if rl.rate == 0 {
		t.Fatal("rate should not be zero")
	}
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	rl := NewRateLimiter(2, 6000.0) // 2 burst, 100/sec refill

	ctx := context.Background()

	// Consume both burst tokens
	rl.Wait(ctx)
	rl.Wait(ctx)

	// Wait 30ms — should refill ~3 tokens at 100/sec
	time.Sleep(30 * time.Millisecond)

	start := time.Now()
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("post-refill wait: %v", err)
	}
	elapsed := time.Since(start)

	// Should return almost immediately since tokens refilled
	if elapsed > 20*time.Millisecond {
		t.Fatalf("expected near-instant after refill, got %v", elapsed)
	}
}
