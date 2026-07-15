package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/handler"
	"github.com/Mrigakshi-RC/vanguard/internal/middleware"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/ratelimit"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/Mrigakshi-RC/vanguard/internal/server"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Postgres connection failed: %v", err)
	}
	defer dbPool.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})

	redisQueue := queue.NewRedisQueue(redisClient, cfg.RedisListKey)
	eventStore := repository.NewPostgresEventStore(dbPool)

	limiter := ratelimit.NewLimiter(redisClient, 10, 20)
	limiterMiddleware := middleware.RateLimitMiddleware(limiter)

	ingestService := service.NewIngestService(redisQueue)
	ingestHandler := handler.NewIngestHandler(ingestService)

	protectedIngestHandler := limiterMiddleware(ingestHandler)

	eventService := service.NewEventService(eventStore)
	getEventHandler := handler.NewGetEventHandler(eventService)

	srv := server.New(server.Routes{
		Ingest:   protectedIngestHandler,
		GetEvent: getEventHandler,
	})

	log.Printf("Server starting on %s...", cfg.HTTPAddr)
	err = http.ListenAndServe(cfg.HTTPAddr, srv)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
