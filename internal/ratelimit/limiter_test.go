package ratelimit

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestLimiter_Allow(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	limiter := NewLimiter(rdb, 1, 2)
	ctx := context.Background()
	key := "rate_limit:test"

	allowed, retryAfter, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("first Allow: %v", err)
	}
	if !allowed || retryAfter != 0 {
		t.Fatalf("first Allow = (%v, %d), want (true, 0)", allowed, retryAfter)
	}

	allowed, retryAfter, err = limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("second Allow: %v", err)
	}
	if !allowed || retryAfter != 0 {
		t.Fatalf("second Allow = (%v, %d), want (true, 0)", allowed, retryAfter)
	}

	allowed, retryAfter, err = limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("third Allow: %v", err)
	}
	if allowed {
		t.Fatal("third Allow should be denied when capacity is 2")
	}
	if retryAfter != 1 {
		t.Fatalf("retryAfter = %d, want 1", retryAfter)
	}
}
