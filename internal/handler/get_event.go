package handler

import (
	"errors"
	"net/http"

	"github.com/Mrigakshi-RC/vanguard/internal/service"
)

type GetEventHandler struct {
	svc *service.EventService
}

func NewGetEventHandler(svc *service.EventService) *GetEventHandler {
	return &GetEventHandler{svc: svc}
}

func (h *GetEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "event id is required")
		return
	}

	event, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidEventID):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrEventNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusOK, event)
}
