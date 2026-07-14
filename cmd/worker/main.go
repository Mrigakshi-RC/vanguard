package main

import (
	"context"
	"log"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Postgres driver setup failed: %v", err)
	}
	defer dbPool.Close()

	redisQueue := queue.NewRedisQueue(cfg.RedisAddr, cfg.RedisListKey)

	store := repository.NewPostgresEventStore(dbPool)
	worker := service.NewWorker(redisQueue, store)

	if err := worker.Run(ctx); err != nil {
		log.Fatalf("Worker loop exited: %v", err)
	}
}
