package service

import (
	"context"
	"log"

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
	}

	_, err = w.store.CreateEvent(ctx, db.CreateEventParams{
		ClientID:  env.ClientID,
		EventType: env.EventType,
		Payload:   env.Payload,
		ReceivedAt: pgtype.Timestamptz{
			Time:  env.ReceivedAt,
			Valid: true,
		},
	})
	if err != nil {
		log.Printf("Database insertion failed: %v", err)
	}
}

func truncateForLog(data []byte, max int) string {
	if len(data) <= max {
		return string(data)
	}
	return string(data[:max]) + "..."
}
