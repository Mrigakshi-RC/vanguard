-- name: CreateEvent :one
INSERT INTO events (
    client_id,
    event_type,
    payload,
    status,
    received_at
) VALUES (
    $1, $2, $3, 'pending', $4
)
RETURNING *;