package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type EventEnvelope struct {
	ID         string          `json:"id"`
	ClientID   string          `json:"client_id"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
	ReceivedAt time.Time       `json:"received_at"`
}

func (r IngestRequest) ToEnvelope() EventEnvelope {
	return EventEnvelope{
		ID:         uuid.NewString(),
		ClientID:   r.ClientID,
		EventType:  r.EventType,
		Payload:    r.Payload,
		ReceivedAt: time.Now().UTC(),
	}
}

func ParseEventEnvelope(data []byte) (EventEnvelope, error) {
	var env EventEnvelope
	err := json.Unmarshal(data, &env)
	return env, err
}
