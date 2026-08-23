package service

//service layer for POST /v1/events
import (
	"context"
	"encoding/json"

	"github.com/Mrigakshi-RC/vanguard/internal/queue"
)

type IngestRequest struct {
	ClientID  string          `json:"client_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

func (r *IngestRequest) Validate() error {
	if r.ClientID == "" {
		return ValidationError{Message: "client_id is required"}
	}
	if r.EventType == "" {
		return ValidationError{Message: "event_type is required"}
	}
	return nil
}

type IngestService struct {
	q queue.Queue
}

func NewIngestService(q queue.Queue) *IngestService {
	return &IngestService{q: q}
}

func (s *IngestService) Ingest(ctx context.Context, req IngestRequest) (string, error) {
	if err := req.Validate(); err != nil {
		return "", err
	}
	env := req.ToEnvelope()
	data, err := json.Marshal(env)
	if err != nil {
		return "", err
	}

	if err := s.q.Enqueue(ctx, data); err != nil {
		return "", QueueError{Cause: err}
	}
	return env.ID, nil
}
