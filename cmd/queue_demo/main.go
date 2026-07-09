package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
)

func main() {
	config := config.Load()

	var q queue.Enqueuer
	q = queue.NewRedisEnqueuer(config.RedisAddr, config.RedisListKey)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	testPayload := []byte(`{"event": "test_connection", "status": "success"}`)

	err := q.Enqueue(ctx, testPayload)
	if err != nil {
		log.Fatalf("Failed to enqueue to Redis: %v", err)
	}

	fmt.Println("Successfully pushed test blob to Redis!")
}
