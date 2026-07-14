# Vanguard

Go service for ingesting and processing events. This README doubles as a dev journal — one entry per day.

---

## Dev journal

### Day 1 — 2026-07-08

**Goal:** Bootstrap the repo with a production-style layout, local infra, schema, and a minimal test harness.

#### Project scaffold

Set up the standard Go layout:

```
vanguard/
├── cmd/vanguard/main.go          # entrypoint (empty main for now)
├── internal/
│   ├── config/                   # env/config loading (stub)
│   ├── handler/                  # HTTP handlers (stub)
│   ├── service/                  # business logic
│   └── server/                   # http.Server setup (stub)
├── db/migrations/                # goose SQL migrations
├── Dockerfile                    # multi-stage image build
├── docker-compose.yaml           # local Postgres + Redis
├── Makefile                      # dev shortcuts
└── go.mod
```

#### Local infrastructure (`docker-compose.yaml`)

- **Postgres 16** — user/db/password: `vanguard`, port `5432`, persistent volume `pgdata`
- **Redis 7** — port `6379`

#### Database schema (goose)

Installed [goose](https://github.com/pressly/goose) and added the first migration:

- `20260708062506_create_events_table.sql`
- Enables `pgcrypto` for UUID generation
- Creates `events` table: `id`, `client_id`, `event_type`, `payload` (JSONB), `status`, `received_at`, `processed_at`, `retry_count`, `last_error`
- Indexes on `status` and `client_id`

#### Makefile targets

| Target | Purpose |
|--------|---------|
| `make up` | Start Postgres + Redis |
| `make down` | Stop containers |
| `make build` | Compile binary to `bin/vanguard` |
| `make run` | Build and run the app |
| `make test` | Run all tests |
| `make tidy` | Tidy go.mod |
| `make migrate-up` | Apply migrations |
| `make migrate-down` | Roll back one migration |
| `make migrate-status` | Show migration state |

#### Tests

Added a table-driven test in `internal/service/service_test.go` for `ValidateEventStatus` — validates allowed statuses: `pending`, `processing`, `completed`.

#### Docker image

Multi-stage `Dockerfile`: compile static binary with Go 1.26 Alpine, run as non-root user on Alpine 3.21.

#### Gotchas learned

- **WSL port 5432 conflict:** Native Postgres on WSL was bound to `127.0.0.1:5432`, so goose/`psql -h localhost` hit the wrong server while `docker compose exec postgres psql` worked fine. Fix: `sudo systemctl stop postgresql` (and optionally `disable` it), then restart compose.
- **goose needs a DB connection string** — `goose up` alone prints usage; use `make migrate-up` or pass `postgres "..."` / set `GOOSE_DRIVER` + `GOOSE_DBSTRING`.
- **`fmt.Println` in tests is hidden** unless you run `go test -v`; prefer `t.Logf` for debug output.

#### Day 1 commands (typical flow)

```bash
make up
make migrate-up
make test
make run
```

---

### Day 2 — 2026-07-09

**Goal:** Prove the ingestion architecture shape — config → service → Redis list — without HTTP or production hardening yet.

#### Config (`internal/config/config.go`)

Replaced the stub with a `Config` struct and `Load()` returning hardcoded local defaults:

| Field | Default | Purpose |
|-------|---------|---------|
| `HTTPAddr` | `:8080` | Future HTTP listen address |
| `RedisAddr` | `localhost:6379` | Redis from `docker-compose` |
| `RedisListKey` | `vanguard:events:ingest` | List name for queued events |

#### Redis queue (`internal/queue/redis.go`)

New package for enqueueing events onto a Redis list:

- `Enqueuer` interface — `Enqueue(ctx, []byte) error` (enables fakes in tests later)
- `RedisEnqueuer` — wraps `github.com/redis/go-redis/v9`
- `LPUSH` onto `REDIS_LIST_KEY` (future worker will `BRPOP` for FIFO)

Added `go-redis/v9` to `go.mod`.

#### Ingest service (`internal/service/ingest.go`)

Started the service layer for event ingestion:

- `IngestRequest` — `client_id`, `event_type`, `payload` (`json.RawMessage`)
- `Validate()` — requires non-empty `client_id` and `event_type`
- `BuildEnvelope()` — adds server-side `received_at` (RFC3339) before enqueue
- `Enqueue()` — marshals envelope JSON and calls `queue.Enqueuer`

Envelope shape written to Redis (matches the `events` table intent):

```json
{
  "client_id": "acme",
  "event_type": "page_view",
  "payload": { "url": "/home" },
  "received_at": "2026-07-09T11:00:00Z"
}
```

#### Queue demo (`cmd/queue_demo/main.go`)

Scratch binary to prove Go → Redis without HTTP:

```bash
make up
go run ./cmd/queue_demo
docker compose exec redis redis-cli LRANGE vanguard:events:ingest 0 -1
```

#### Still stubs / not wired yet

- `internal/handler/` — no `POST /v1/events` handler
- `internal/server/` — no mux or `http.Server`
- `cmd/vanguard/main.go` — empty entrypoint (no `func main` yet)

#### Gotchas learned

- **Package-level `:=` is invalid** — short assignment only works inside functions; use `var`, `const`, or a `Load()` that returns a struct.
- **`main` belongs in `cmd/`** — `func main()` inside `internal/queue` does not produce a runnable binary; use `cmd/queue_demo` for scratch programs.
- **Listen address needs a colon** — `":8080"` for `http.ListenAndServe`, not `"8080"`.
- **`LPUSH` returns a status, not an error** — call `.Err()` on the Redis result to surface failures.

#### Day 2 commands (typical flow)

```bash
make up
go run ./cmd/queue_demo
docker compose exec redis redis-cli LRANGE vanguard:events:ingest 0 -1
```

---

### Day 3 — 2026-07-11

**Goal:** Wire the full HTTP ingestion path — `POST /v1/events` → handler → service → Redis list.

#### HTTP handler (`internal/handler/ingest.go`)

New `IngestHandler` implementing `http.Handler`:

- Decodes JSON body into `service.IngestRequest`
- Calls `IngestService.Ingest`
- Returns `202 Accepted` + `{"status":"queued"}` on success
- Returns `400 Bad Request` for invalid JSON or validation errors
- Returns `405 Method Not Allowed` for non-POST requests

#### Ingest service (`internal/service/ingest.go`)

Refactored Day 2 request helpers into a proper service struct:

- `IngestService` — holds `queue.Enqueuer`, connects handler to Redis
- `NewIngestService(q)` — constructor injection of the queue
- `Ingest(ctx, req)` — `Validate()` → `BuildEnvelope()` → `Enqueue()`

#### App entrypoint (`cmd/vanguard/main.go`)

Wired the full dependency chain and started the server:

```
config.Load()
  → queue.NewRedisEnqueuer
  → service.NewIngestService
  → handler.NewIngestHandler
  → http.ServeMux ("POST /v1/events")
  → http.ListenAndServe
```

The ingestion architecture proof is now end-to-end over HTTP.

#### Still deferred

- `internal/server/` — routes live in `main` for now (no separate server package)
- No Redis `Ping` at startup — server starts even if Redis is down
- All service errors map to `400` (Redis failures should be `503` later)
- No handler/service tests for ingest yet
- No graceful shutdown, auth, or rate limiting

#### Gotchas learned

- **`http.Handler` vs handler func** — `IngestHandler` is a struct with `ServeHTTP`; register it directly on the mux with `mux.Handle("POST /v1/events", ingestHandler)`.
- **Go 1.22+ route patterns** — `"POST /v1/events"` binds method and path in one string; no separate `HandleFunc` + manual method check needed (handler still guards non-POST as a fallback).
- **Write headers before body** — call `w.WriteHeader` before `w.Write`; set `Content-Type` early.
- **Transitive deps in `go.mod`** — `xxhash` and `atomic` are pulled in by `go-redis/v9`, not used directly.

#### Day 3 commands (typical flow)

```bash
make up
make run

curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"test","event_type":"ping","payload":{"n":1}}'

docker compose exec redis redis-cli LRANGE vanguard:events:ingest 0 -1
```

---

### Day 4 — 2026-07-14

**Goal:** Add the worker path — Redis list → Postgres — using sqlc for typed SQL and a thin repository wrapper.

#### Config (`internal/config/config.go`)

Extended `Config` with Postgres connection settings:

| Field | Default | Purpose |
|-------|---------|---------|
| `PostgresDSN` | `postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable` | pgx pool connection string |

#### Redis queue refactor (`internal/queue/redis.go`)

Merged enqueue/dequeue into a single abstraction:

- `Queue` interface — `Enqueue(ctx, []byte) error` + `Dequeue(ctx) ([]byte, error)`
- `RedisQueue` — one struct/client for both sides
- `LPUSH` on enqueue, `BRPOP` (timeout `0`) on dequeue — FIFO with Day 2's push-left pattern
- `cmd/vanguard/main.go` updated to `NewRedisQueue` (replacing separate enqueuer type)

#### sqlc data layer

Boot.dev-style sqlc setup for Postgres access:

- `sqlc.yaml` — schema from `db/migrations/`, queries from `db/queries/`, generates into `internal/db/` with `pgx/v5`
- `db/queries/events.sql` — `CreateEvent :one` INSERT with `status = 'pending'` and `RETURNING *`
- Generated code: `internal/db/` (`Queries`, `Event`, `CreateEventParams`)

Added `github.com/jackc/pgx/v5` (via sqlc + worker).

#### Repository (`internal/repository/event.go`)

Thin wrapper over sqlc-generated queries:

- `EventStore` interface — `CreateEvent(ctx, db.CreateEventParams) (db.Event, error)`
- `PostgresEventStore` — holds `*db.Queries`, delegates to `CreateEvent`

Keeps the worker service testable without hand-writing SQL in Go.

#### Worker service (`internal/service/worker.go`)

New background consumer:

- `EventEnvelope` — mirrors the Redis JSON envelope (`client_id`, `event_type`, `payload`, `received_at`)
- `Worker` — holds `queue.Queue` + `repository.EventStore`
- `Run(ctx)` — infinite loop: `Dequeue` → `processOne`
- `processOne` — unmarshal envelope → `store.CreateEvent` with `pgtype.Timestamptz`

#### Worker entrypoint (`cmd/worker/main.go`)

Separate binary from the HTTP API:

```
config.Load()
  → pgxpool.New(PostgresDSN)
  → queue.NewRedisQueue
  → repository.NewPostgresEventStore
  → service.NewWorker
  → worker.Run
```

Run API and worker as two processes for the full ingest → persist path.

#### End-to-end flow (Day 1–4)

```
POST /v1/events → Redis list (LPUSH) → worker (BRPOP) → sqlc CreateEvent → events table
```

#### Still deferred

- No `make sqlc` target — run `sqlc generate` manually after query/schema changes
- Worker has no graceful shutdown (no signal handling / context cancel)
- Bad JSON and DB failures are logged or dropped — no dead-letter queue or requeue
- `Dequeue` errors are swallowed with `continue` (no ctx check on error path)
- No worker or repository tests yet
- `internal/server/` still unused

#### Gotchas learned

- **sqlc + goose** — point `schema` at migration files; sqlc reads the DDL, goose applies it at runtime
- **`CreateEventParams.Payload` is `[]byte`** — pass `json.RawMessage` from the envelope directly; pgx maps it to JSONB
- **`received_at` needs `pgtype.Timestamptz`** — sqlc generates pgx types, not plain `time.Time`, for nullable/timestamp columns
- **`RETURNING *` vs explicit columns** — sqlc may emit a partial `Scan` if the table has columns not listed in RETURNING; keep query and migration in sync
- **Separate `cmd/worker`** — same pattern as `cmd/vanguard`; `internal/` packages are shared, binaries are not
- **FIFO** — `LPUSH` + `BRPOP` is the correct pair; do not `BRPOP` from the same end you push

#### Day 4 commands (typical flow)

```bash
make up
make migrate-up
sqlc generate

# terminal 1 — API
make run

# terminal 2 — worker
go run ./cmd/worker

curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"test","event_type":"ping","payload":{"n":1}}'

docker compose exec redis redis-cli LLEN vanguard:events:ingest

docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id, client_id, event_type, status, received_at FROM events;"
```

---
