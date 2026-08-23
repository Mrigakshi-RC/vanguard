package queue

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Queue interface {
	Enqueue(ctx context.Context, data []byte) error
	Dequeue(ctx context.Context) ([]byte, error)
	Requeue(ctx context.Context, data []byte) error
	EnqueueDLQ(ctx context.Context, data []byte) error
}

type RedisQueue struct {
	redis   *redis.Client
	listKey string
	dlqKey  string
}

func NewRedisQueue(client *redis.Client, listKey string, dlqKey string) *RedisQueue {
	return &RedisQueue{
		redis:   client,
		listKey: listKey,
		dlqKey:  dlqKey,
	}
}

func (r *RedisQueue) Enqueue(ctx context.Context, data []byte) error {
	return r.redis.LPush(ctx, r.listKey, data).Err() //why.Err()? -> because redis.LPush returns a redis.Status, and we want to return an error
}

func (r *RedisQueue) Dequeue(ctx context.Context) ([]byte, error) {
	for ctx.Err() == nil {
		result, err := r.redis.BRPop(ctx, 1*time.Second, r.listKey).Result()
		if err == redis.Nil {
			continue // Timeout reached, loop and check ctx again if the process has been cancelled
		}
		if err != nil {
			return nil, err // Context cancelled or connection error
		}
		return []byte(result[1]), nil
	}
	return nil, ctx.Err()
}

func (r *RedisQueue) Requeue(ctx context.Context, data []byte) error {
	return r.Enqueue(ctx, data)
}

func (r *RedisQueue) EnqueueDLQ(ctx context.Context, data []byte) error {
	return r.redis.LPush(ctx, r.dlqKey, data).Err()
}
