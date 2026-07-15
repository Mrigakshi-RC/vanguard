package queue

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type Queue interface {
	Enqueue(ctx context.Context, data []byte) error
	Dequeue(ctx context.Context) ([]byte, error)
}

type RedisQueue struct {
	redis   *redis.Client
	listKey string
}

func NewRedisQueue(client *redis.Client, listKey string) *RedisQueue {
	return &RedisQueue{
		redis:   client,
		listKey: listKey,
	}
}

func (r *RedisQueue) Enqueue(ctx context.Context, data []byte) error {
	return r.redis.LPush(ctx, r.listKey, data).Err() //why.Err()? -> because redis.LPush returns a redis.Status, and we want to return an error
}

func (r *RedisQueue) Dequeue(ctx context.Context) ([]byte, error) {
	result, err := r.redis.BRPop(ctx, 0, r.listKey).Result()
	if err != nil {
		return nil, err
	}
	// result[0] = key, result[1] = value
	return []byte(result[1]), nil
}
