package ratelimit

import (
	"context"
	_ "embed"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed token_bucket.lua
var tokenBucketScript string

type Limiter struct {
	rdb      *redis.Client
	rate     int
	capacity int
}

func NewLimiter(rdb *redis.Client, rate int, capacity int) *Limiter {
	return &Limiter{
		rdb:      rdb,
		rate:     rate,
		capacity: capacity,
	}
}

func (l *Limiter) Allow(ctx context.Context, key string) (bool, int, error) {
	now := time.Now().Unix()
	res, err := l.rdb.Eval(ctx, tokenBucketScript, []string{key}, l.capacity, l.rate, now, 1).Result()
	if err != nil {
		return false, 0, err
	}

	// The Lua script returns 1 if allowed, 0 if rate-limited
	allowed, ok := res.(int64)
	if ok && allowed == 1 {
		return true, 0, nil
	}

	// Calculate baseline retry-after window in seconds if rejected
	retryAfter := int(math.Ceil(1.0 / float64(l.rate)))
	retryAfter = max(retryAfter, 1)

	return false, retryAfter, nil

}
