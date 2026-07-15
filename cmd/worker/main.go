package main

import (
	"context"
	"log"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Postgres driver setup failed: %v", err)
	}
	defer dbPool.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	redisQueue := queue.NewRedisQueue(redisClient, cfg.RedisListKey)

	store := repository.NewPostgresEventStore(dbPool)
	worker := service.NewWorker(redisQueue, store)

	if err := worker.Run(ctx); err != nil {
		log.Fatalf("Worker loop exited: %v", err)
	}
}
