package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Postgres driver setup failed: %v", err)
	}
	defer dbPool.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	redisQueue := queue.NewRedisQueue(redisClient, cfg.RedisListKey, cfg.RedisDLQKey)

	store := repository.NewPostgresEventStore(dbPool)
	worker := service.NewWorker(redisQueue, store)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := worker.Run(ctx); err != nil && err != context.Canceled {
			log.Fatalf("Worker loop exited: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("Shutting down worker...")
	cancel()
	wg.Wait()

	log.Println("Worker shutdown complete")
}
