package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/Mrigakshi-RC/vanguard/internal/service"
)

type stubQueue struct {
	err error
}

func (s stubQueue) Enqueue(ctx context.Context, data []byte) error {
	return s.err
}

func (s stubQueue) Dequeue(ctx context.Context) ([]byte, error) {
	return nil, nil
}

func (s stubQueue) Requeue(ctx context.Context, data []byte) error {
	return s.err
}

func (s stubQueue) EnqueueDLQ(ctx context.Context, data []byte) error {
	return s.err
}

func TestIngestHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		queueErr   error
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid json",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid JSON body",
		},
		{
			name:       "missing client_id",
			body:       `{"event_type":"ping"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "client_id is required",
		},
		{
			name:       "missing event_type",
			body:       `{"client_id":"acme"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "event_type is required",
		},
		{
			name:       "queue failure",
			body:       `{"client_id":"acme","event_type":"ping","payload":{"n":1}}`,
			queueErr:   errors.New("redis down"),
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "service temporarily unavailable",
		},
		{
			name:       "accepted",
			body:       `{"client_id":"acme","event_type":"ping","payload":{"n":1}}`,
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewIngestService(stubQueue{err: tt.queueErr})
			h := NewIngestHandler(svc)

			req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(tt.body))
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

			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["status"] != "queued" {
				t.Errorf("status = %q, want queued", body["status"])
			}
			if tt.wantStatus == http.StatusAccepted {
				if body["id"] == "" {
					t.Error("id missing from 202 response")
				}
				if _, err := uuid.Parse(body["id"]); err != nil {
					t.Errorf("id = %q, want valid UUID: %v", body["id"], err)
				}
			}
		})
	}
}

func TestWriteJSONError_escapesQuotes(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSONError(rec, http.StatusBadRequest, `bad "input"`)

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != `bad "input"` {
		t.Errorf("error = %q, want quoted message preserved", body["error"])
	}
}
