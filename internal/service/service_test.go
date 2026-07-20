package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Mrigakshi-RC/vanguard/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestEventEnvelope_roundTrip(t *testing.T) {
	req := IngestRequest{
		ClientID:  "acme",
		EventType: "page_view",
		Payload:   json.RawMessage(`{"url":"/home"}`),
	}

	data, err := json.Marshal(req.ToEnvelope())
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	got, err := ParseEventEnvelope(data)
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}

	if got.ClientID != req.ClientID {
		t.Errorf("ClientID = %q, want %q", got.ClientID, req.ClientID)
	}
	if got.EventType != req.EventType {
		t.Errorf("EventType = %q, want %q", got.EventType, req.EventType)
	}
	if string(got.Payload) != string(req.Payload) {
		t.Errorf("Payload = %s, want %s", got.Payload, req.Payload)
	}
	if got.ReceivedAt.IsZero() {
		t.Error("ReceivedAt is zero")
	}
}

func TestTruncateForLog(t *testing.T) {
	short := []byte("ok")
	if got := truncateForLog(short, 256); got != "ok" {
		t.Errorf("truncateForLog(short) = %q, want ok", got)
	}

	long := []byte("abcdefghijklmnopqrstuvwxyz")
	if got := truncateForLog(long, 10); got != "abcdefghij..." {
		t.Errorf("truncateForLog(long) = %q, want truncated", got)
	}
}

func TestEventService_GetByID(t *testing.T) {
	const validID = "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name       string
		id         string
		storeErr   error
		wantErr    bool
		wantClient string
	}{
		{name: "invalid id", id: "not-a-uuid", wantErr: true},
		{name: "not found", id: validID, storeErr: pgx.ErrNoRows, wantErr: true},
		{name: "found", id: validID, wantClient: "acme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventService(stubEventStore{err: tt.storeErr})
			got, err := svc.GetByID(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Fatalf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantClient != "" && got.ClientID != tt.wantClient {
				t.Errorf("ClientID = %q, want %q", got.ClientID, tt.wantClient)
			}
		})
	}
}

type stubEventStore struct {
	err error
}

func (s stubEventStore) CreateEvent(ctx context.Context, arg db.CreateEventParams) (db.Event, error) {
	return db.Event{}, nil
}

func (s stubEventStore) GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error) {
	if s.err != nil {
		return db.Event{}, s.err
	}

	receivedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	return db.Event{
		ID:         id,
		ClientID:   "acme",
		EventType:  "page_view",
		Payload:    []byte(`{"url":"/home"}`),
		Status:     "pending",
		ReceivedAt: pgtype.Timestamptz{Time: receivedAt, Valid: true},
	}, nil
}

type failingEventStore struct {
	failCount int
	failErr   error
	calls     int
}

func (s *failingEventStore) CreateEvent(ctx context.Context, arg db.CreateEventParams) (db.Event, error) {
	s.calls++
	if s.calls <= s.failCount {
		return db.Event{}, s.failErr
	}
	return db.Event{}, nil
}
func (s *failingEventStore) GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error) {
	return db.Event{}, nil
}

type recordingQueue struct {
	requeued int
	dlq      int
}

func (q *recordingQueue) Enqueue(ctx context.Context, data []byte) error    { return nil }
func (q *recordingQueue) Dequeue(ctx context.Context) ([]byte, error)       { return nil, nil }
func (q *recordingQueue) Requeue(ctx context.Context, data []byte) error    { q.requeued++; return nil }
func (q *recordingQueue) EnqueueDLQ(ctx context.Context, data []byte) error { q.dlq++; return nil }

func validEnvelope(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(IngestRequest{
		ClientID: "acme", EventType: "ping", Payload: json.RawMessage(`{"n":1}`),
	}.ToEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWorker_malformedJSONGoesToDLQ(t *testing.T) {
	q := &recordingQueue{}
	store := &failingEventStore{}

	NewWorker(q, store).processOne(context.Background(), []byte("{bad"))

	if store.calls != 0 || q.dlq != 1 || q.requeued != 0 {
		t.Fatalf("calls=%d dlq=%d requeued=%d", store.calls, q.dlq, q.requeued)
	}
}

func TestWorker_retriesThenSucceeds(t *testing.T) {
	store := &failingEventStore{failCount: 2, failErr: errors.New("connection refused")}
	q := &recordingQueue{}

	NewWorker(q, store).processOne(context.Background(), validEnvelope(t))

	if store.calls != 3 || q.requeued != 0 || q.dlq != 0 {
		t.Fatalf("calls=%d requeued=%d dlq=%d", store.calls, q.requeued, q.dlq)
	}
}

func TestWorker_exhaustedRetriesRequeue(t *testing.T) {
	t.Setenv("VANGUARD_RETRY_MAX_ATTEMPTS", "2")

	store := &failingEventStore{failCount: 10, failErr: errors.New("connection refused")}
	q := &recordingQueue{}

	NewWorker(q, store).processOne(context.Background(), validEnvelope(t))

	if store.calls != 2 || q.requeued != 1 || q.dlq != 0 {
		t.Fatalf("calls=%d requeued=%d dlq=%d", store.calls, q.requeued, q.dlq)
	}
}
