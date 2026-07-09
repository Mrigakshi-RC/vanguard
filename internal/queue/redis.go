package queue

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type Enqueuer interface {
	Enqueue(ctx context.Context, data []byte) error
}

type RedisEnqueuer struct {
	redis   *redis.Client
	listKey string
}

func NewRedisEnqueuer(addr string, listKey string) *RedisEnqueuer {
	return &RedisEnqueuer{
		redis: redis.NewClient(&redis.Options{
			Addr: addr,
		}),
		listKey: listKey,
	}
}

func (r *RedisEnqueuer) Enqueue(ctx context.Context, data []byte) error {
	return r.redis.LPush(ctx, r.listKey, data).Err() //why.Err()? -> because redis.LPush returns a redis.Status, and we want to return an error
}
