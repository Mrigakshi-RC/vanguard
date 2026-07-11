package main

import (
	"log"
	"net/http"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/handler"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
)

func main() {
	cfg := config.Load()

	redisQueue := queue.NewRedisEnqueuer(cfg.RedisAddr, cfg.RedisListKey)
	ingestService := service.NewIngestService(redisQueue)
	ingestHandler := handler.NewIngestHandler(ingestService)

	mux := http.NewServeMux()
	mux.Handle("POST /v1/events", ingestHandler)

	log.Printf("Server starting on %s...", cfg.HTTPAddr)
	err := http.ListenAndServe(cfg.HTTPAddr, mux)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
