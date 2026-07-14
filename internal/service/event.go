package service

//service layer for GET  /v1/events/:id
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/db"
	"github.com/Mrigakshi-RC/vanguard/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrEventNotFound  = errors.New("event not found")
	ErrInvalidEventID = errors.New("invalid event id")
)

type EventResponse struct {
	ID          string          `json:"id"`
	ClientID    string          `json:"client_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	ReceivedAt  time.Time       `json:"received_at"`
	ProcessedAt *time.Time      `json:"processed_at,omitempty"`
}

type EventService struct {
	store repository.EventStore
}

func NewEventService(store repository.EventStore) *EventService {
	return &EventService{store: store}
}

func (s *EventService) GetByID(ctx context.Context, id string) (EventResponse, error) {
	var eventID pgtype.UUID
	if err := eventID.Scan(id); err != nil {
		return EventResponse{}, ErrInvalidEventID
	}

	event, err := s.store.GetEventByID(ctx, eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EventResponse{}, ErrEventNotFound
		}
		return EventResponse{}, fmt.Errorf("get event: %w", err)
	}

	return eventToResponse(event)
}

func eventToResponse(event db.Event) (EventResponse, error) {
	if !event.ID.Valid {
		return EventResponse{}, fmt.Errorf("event id is invalid")
	}

	resp := EventResponse{
		ID:         event.ID.String(),
		ClientID:   event.ClientID,
		EventType:  event.EventType,
		Payload:    json.RawMessage(event.Payload),
		Status:     event.Status,
		ReceivedAt: event.ReceivedAt.Time,
	}

	if event.ProcessedAt.Valid {
		processedAt := event.ProcessedAt.Time
		resp.ProcessedAt = &processedAt
	}

	return resp, nil
}
