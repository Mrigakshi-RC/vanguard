package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type HealthHandler struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func NewHealthHandler(db *pgxpool.Pool, redis *redis.Client) *HealthHandler {
	return &HealthHandler{db: db, redis: redis}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.db.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("postgres unavailable"))
		return
	}
	if err := h.redis.Ping(ctx).Err(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("redis unavailable"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
