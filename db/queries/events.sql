-- name: CreateEvent :one
INSERT INTO events (
    id,
    client_id,
    event_type,
    payload,
    status,
    received_at
) VALUES (
    $1, $2, $3, $4, 'pending', $5
)
RETURNING *;

-- name: GetEventByID :one
SELECT id, client_id, event_type, payload, status, received_at, processed_at
FROM events
WHERE id = $1;