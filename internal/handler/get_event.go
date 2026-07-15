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
		WriteJSONError(w, http.StatusBadRequest, "event id is required")
		return
	}

	event, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidEventID):
			WriteJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrEventNotFound):
			WriteJSONError(w, http.StatusNotFound, err.Error())
		default:
			WriteJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	WriteJSON(w, http.StatusOK, event)
}
