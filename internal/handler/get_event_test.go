package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/db"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubEventStore struct {
	event db.Event
	err   error
}

func (s *stubEventStore) CreateEvent(ctx context.Context, arg db.CreateEventParams) (db.Event, error) {
	return db.Event{}, nil
}

func (s *stubEventStore) GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error) {
	if s.err != nil {
		return db.Event{}, s.err
	}
	return s.event, nil
}

func TestGetEventHandler(t *testing.T) {
	validID := "550e8400-e29b-41d4-a716-446655440000"
	var uid pgtype.UUID
	if err := uid.Scan(validID); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}

	receivedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		id         string
		store      *stubEventStore
		wantStatus int
		wantError  string
	}{
		{
			name:       "missing id",
			id:         "",
			store:      &stubEventStore{},
			wantStatus: http.StatusBadRequest,
			wantError:  "event id is required",
		},
		{
			name:       "invalid id",
			id:         "not-a-uuid",
			store:      &stubEventStore{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid event id",
		},
		{
			name: "not found",
			id:   validID,
			store: &stubEventStore{
				err: pgx.ErrNoRows,
			},
			wantStatus: http.StatusNotFound,
			wantError:  "event not found",
		},
		{
			name: "found",
			id:   validID,
			store: &stubEventStore{
				event: db.Event{
					ID:         uid,
					ClientID:   "acme",
					EventType:  "ping",
					Payload:    []byte(`{"n":1}`),
					Status:     "pending",
					ReceivedAt: pgtype.Timestamptz{Time: receivedAt, Valid: true},
				},
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewEventService(tt.store)
			h := NewGetEventHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/v1/events/"+tt.id, nil)
			req.SetPathValue("id", tt.id)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantError != "" {
				var body map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["error"] != tt.wantError {
					t.Errorf("error = %q, want %q", body["error"], tt.wantError)
				}
				return
			}

			var got service.EventResponse
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.ID != validID {
				t.Errorf("ID = %q, want %q", got.ID, validID)
			}
		})
	}
}
