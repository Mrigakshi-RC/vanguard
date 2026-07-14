package repository

import (
	"context"

	"github.com/Mrigakshi-RC/vanguard/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventStore interface {
	CreateEvent(ctx context.Context, arg db.CreateEventParams) (db.Event, error)
	GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error)
}

type PostgresEventStore struct {
	queries *db.Queries
}

func NewPostgresEventStore(sqlDB db.DBTX) *PostgresEventStore {
	return &PostgresEventStore{
		queries: db.New(sqlDB),
	}
}

func (s *PostgresEventStore) CreateEvent(ctx context.Context, arg db.CreateEventParams) (db.Event, error) {
	event, err := s.queries.CreateEvent(ctx, arg)
	if err != nil {
		return db.Event{}, err
	}
	return event, nil
}

func (s *PostgresEventStore) GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error) {
	event, err := s.queries.GetEventByID(ctx, id)
	if err != nil {
		return db.Event{}, err
	}
	return event, nil
}
