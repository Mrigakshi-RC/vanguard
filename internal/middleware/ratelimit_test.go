package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubLimiter struct {
	allowed    bool
	retryAfter int
	err        error
}

func (s stubLimiter) Allow(ctx context.Context, key string) (bool, int, error) {
	return s.allowed, s.retryAfter, s.err
}

func TestRateLimitMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		limiter    stubLimiter
		wantStatus int
		wantError  string
	}{
		{
			name:       "allowed passes through",
			limiter:    stubLimiter{allowed: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "denied returns 429",
			limiter:    stubLimiter{allowed: false, retryAfter: 1},
			wantStatus: http.StatusTooManyRequests,
			wantError:  "Too many requests, retry after 1",
		},
		{
			name:       "limiter error fails open",
			limiter:    stubLimiter{err: errors.New("redis down")},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := RateLimitMiddleware(tt.limiter)
			req := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rec := httptest.NewRecorder()

			mw(okHandler).ServeHTTP(rec, req)

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
			}
		})
	}
}
