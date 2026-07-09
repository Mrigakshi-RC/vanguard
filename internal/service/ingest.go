package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/queue"
)

type IngestRequest struct {
	ClientID  string          `json:"client_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

func (r *IngestRequest) Validate() error {
	if r.ClientID == "" {
		return errors.New("client_id is required")
	}
	if r.EventType == "" {
		return errors.New("event_type is required")
	}
	return nil
}

func (r *IngestRequest) BuildEnvelope() ([]byte, error) {
	envelope, err := json.Marshal(map[string]any{
		"client_id":   r.ClientID,
		"event_type":  r.EventType,
		"payload":     r.Payload,
		"received_at": time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	return envelope, nil
}

func (r *IngestRequest) Enqueue(ctx context.Context, q queue.Enqueuer) error {
	envelope, err := r.BuildEnvelope()
	if err != nil {
		return err
	}
	return q.Enqueue(ctx, envelope)
}
