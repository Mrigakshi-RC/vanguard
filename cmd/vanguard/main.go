package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/handler"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/Mrigakshi-RC/vanguard/internal/server"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Postgres connection failed: %v", err)
	}
	defer dbPool.Close()

	redisQueue := queue.NewRedisQueue(cfg.RedisAddr, cfg.RedisListKey)
	eventStore := repository.NewPostgresEventStore(dbPool)

	ingestService := service.NewIngestService(redisQueue)
	ingestHandler := handler.NewIngestHandler(ingestService)

	eventService := service.NewEventService(eventStore)
	getEventHandler := handler.NewGetEventHandler(eventService)

	srv := server.New(server.Routes{
		Ingest:   ingestHandler,
		GetEvent: getEventHandler,
	})

	log.Printf("Server starting on %s...", cfg.HTTPAddr)
	err = http.ListenAndServe(cfg.HTTPAddr, srv)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
