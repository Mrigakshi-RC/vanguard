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

	for ctx.Err() == nil {
		data, err := w.q.Dequeue(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		w.processOne(ctx, data)
	}

	return ctx.Err()
}

func (w *Worker) processOne(ctx context.Context, data []byte) {
	handedOff := false

	defer func() {
		if handedOff {
			return
		}
		if ctx.Err() != nil {
			log.Printf("Shutdown during processOne, requeuing message")
			if err := w.q.Requeue(context.Background(), data); err != nil {
				log.Printf("Failed to requeue on shutdown: %v", err)
			}
		}
	}()
	env, err := ParseEventEnvelope(data)
	if err != nil {
		log.Printf("Malformed event envelope: %v body=%s", err, truncateForLog(data, 256))
		if dlqErr := w.sendToDLQ(ctx, data); dlqErr != nil {
			log.Printf("Failed to send malformed event to DLQ: %v", dlqErr)
		}
		handedOff = true
		return
	}

	cfg := config.Load()

	var dbErr error
	for retryCount := 0; retryCount < cfg.RetryMaxAttempts; retryCount++ {
		var pgUUID pgtype.UUID
		err := pgUUID.Scan(env.ID)
		if err != nil {
			log.Printf("failed to parse uuid string: %v", err)
			if dlqErr := w.sendToDLQ(ctx, data); dlqErr != nil {
				log.Printf("Failed to send malformed event to DLQ: %v", dlqErr)
			}
			handedOff = true
			return
		}
		_, dbErr = w.store.CreateEvent(ctx, db.CreateEventParams{
			ID:        pgUUID,
			ClientID:  env.ClientID,
			EventType: env.EventType,
			Payload:   env.Payload,
			ReceivedAt: pgtype.Timestamptz{
				Time:  env.ReceivedAt,
				Valid: true,
			},
		})
		if dbErr == nil {
			handedOff = true
			return
		}
		if !isTransientError(dbErr) {
			log.Printf("Permanent database error encountered: %v. Routing to DLQ.", dbErr)
			if dlqErr := w.sendToDLQ(ctx, data); dlqErr != nil {
				log.Printf("Failed to send permanent failure to DLQ: %v", dlqErr)
			}
			handedOff = true
			return
		}
		if retryCount < cfg.RetryMaxAttempts-1 {
			delay := time.Duration(1<<uint(retryCount)) * time.Second
			maxDelay := time.Duration(cfg.RetryMaxDelay) * time.Second
			delay = min(delay, maxDelay)

			log.Printf("Transient DB error: %v. Retrying in %v (Attempt %d/%d)", dbErr, delay, retryCount+1, cfg.RetryMaxAttempts)

			select {
			case <-ctx.Done():
				//handled by defer at the start of processOne
				return
			case <-time.After(delay):
				continue
			}
		}
	}

	log.Printf("Database insertion failed after %d attempts: %v, requeuing to Redis", cfg.RetryMaxAttempts, dbErr)
	if err := w.q.Requeue(ctx, data); err != nil {
		log.Printf("Failed to requeue event to redis: %v", err)
		return
	}
	handedOff = true
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
	for _, substring := range []string{"connection", "timeout", "deadlock", "eof", "refused", "starting up"} {
		if strings.Contains(errMsg, substring) {
			return true
		}
	}
	return false
}
