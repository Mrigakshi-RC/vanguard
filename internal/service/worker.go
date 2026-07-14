package service

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/db"
	"github.com/Mrigakshi-RC/vanguard/internal/queue"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventEnvelope struct {
	ClientID   string          `json:"client_id"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
	ReceivedAt time.Time       `json:"received_at"`
}

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
			continue
		}

		w.processOne(ctx, data)
	}
}

func (w *Worker) processOne(ctx context.Context, data []byte) error {
	var env EventEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}

	_, err := w.store.CreateEvent(ctx, db.CreateEventParams{
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

	return nil
}
