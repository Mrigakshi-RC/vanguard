package service

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/config"
	"github.com/Mrigakshi-RC/vanguard/internal/db"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
)

type Worker struct {
	q     queue.Queue
	store repository.EventStore
}

func NewWorker(q queue.Queue, store repository.EventStore) *Worker {
	return &Worker{
		q:     q,
		store: store,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	log.Println("Worker service started successfully...")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := w.q.Dequeue(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		w.processOne(ctx, data)
	}
}

func (w *Worker) processOne(ctx context.Context, data []byte) {
	env, err := ParseEventEnvelope(data)
	if err != nil {
		log.Printf("Malformed event envelope: %v body=%s", err, truncateForLog(data, 256))
		if dlqErr := w.sendToDLQ(ctx, data); dlqErr != nil {
			log.Printf("Failed to send malformed event to DLQ: %v", dlqErr)
		}
		return
	}

	cfg := config.Load()

	var dbErr error
	for retryCount := 0; retryCount < cfg.RetryMaxAttempts; retryCount++ {
		_, dbErr = w.store.CreateEvent(ctx, db.CreateEventParams{
			ClientID:  env.ClientID,
			EventType: env.EventType,
			Payload:   env.Payload,
			ReceivedAt: pgtype.Timestamptz{
				Time:  env.ReceivedAt,
				Valid: true,
			},
		})
		if dbErr == nil {
			break
		}
		if !isTransientError(dbErr) {
			log.Printf("Permanent database error encountered: %v. Routing to DLQ.", dbErr)
			if dlqErr := w.sendToDLQ(ctx, data); dlqErr != nil {
				log.Printf("Failed to send permanent failure to DLQ: %v", dlqErr)
			}
			return
		}
		if retryCount < cfg.RetryMaxAttempts-1 {
			delay := time.Duration(1<<uint(retryCount)) * time.Second
			maxDelay := time.Duration(cfg.RetryMaxDelay) * time.Second
			if delay > maxDelay*time.Second {
				delay = maxDelay * time.Second
			}

			log.Printf("Transient DB error: %v. Retrying in %v (Attempt %d/%d)", dbErr, delay, retryCount+1, cfg.RetryMaxAttempts)

			select {
			case <-ctx.Done():
				log.Printf("Context cancelled during retry backoff: %v", ctx.Err())
				_ = w.q.Requeue(context.Background(), data)
				return
			case <-time.After(delay):
				continue
			}
		}
	}

	log.Printf("Database insertion failed after %d attempts: %v, requeuing to Redis", cfg.RetryMaxAttempts, dbErr)
	if err := w.q.Requeue(ctx, data); err != nil {
		log.Printf("Failed to requeue event to redis: %v", err)
	}
}

func truncateForLog(data []byte, max int) string {
	if len(data) <= max {
		return string(data)
	}
	return string(data[:max]) + "..."
}

func (w *Worker) sendToDLQ(ctx context.Context, data []byte) error {
	err := w.q.EnqueueDLQ(ctx, data)
	if err != nil {
		return err
	}
	return nil
}

func isTransientError(err error) bool {
	errMsg := err.Error()
	for _, substring := range []string{"connection", "timeout", "deadlock", "eof", "refused"} {
		if strings.Contains(errMsg, substring) {
			return true
		}
	}
	return false
}
