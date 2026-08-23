package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})

	defer redisClient.Close()
	defer dbPool.Close()

	redisQueue := queue.NewRedisQueue(redisClient, cfg.RedisListKey, cfg.RedisDLQKey)
	eventStore := repository.NewPostgresEventStore(dbPool)

	ingestService := service.NewIngestService(redisQueue)
	ingestHandler := handler.NewIngestHandler(ingestService)

	var protectedIngestHandler http.Handler = ingestHandler
	if cfg.RateLimitEnabled {
		limiter := ratelimit.NewLimiter(redisClient, cfg.RateLimitRate, cfg.RateLimitCapacity)
		protectedIngestHandler = middleware.RateLimitMiddleware(limiter)(ingestHandler)
	}

	eventService := service.NewEventService(eventStore)
	getEventHandler := handler.NewGetEventHandler(eventService)
	healthHandler := handler.NewHealthHandler(dbPool, redisClient)

	routes := server.New(server.Routes{
		Health:   healthHandler,
		Ingest:   protectedIngestHandler,
		GetEvent: getEventHandler,
	})
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: routes,
	}

	log.Printf("Server starting on %s...", cfg.HTTPAddr)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Shutdown gracefully
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	//listening on the main thread since the server is running in a background goroutine
	<-stop
	log.Println("Shutting down server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server gracefully stopped. Zero active connections left.")
}
