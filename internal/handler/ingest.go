package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mrigakshi-RC/vanguard/internal/service"
)

type IngestHandler struct {
	svc *service.IngestService
}

func NewIngestHandler(svc *service.IngestService) *IngestHandler {
	return &IngestHandler{svc: svc}
}

func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req service.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := h.svc.Ingest(r.Context(), req); err != nil {
		var validationErr service.ValidationError
		var queueErr service.QueueError
		switch {
		case errors.As(err, &validationErr):
			writeJSONError(w, http.StatusBadRequest, validationErr.Message)
		case errors.As(err, &queueErr):
			writeJSONError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		default:
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
