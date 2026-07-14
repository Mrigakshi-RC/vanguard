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
- Creates `events` table: `id` (UUID), `client_id` (TEXT), `event_type` (TEXT), `payload` (JSONB), `status` (TEXT, default `pending`), `received_at` (TIMESTAMPTZ), `processed_at` (TIMESTAMPTZ, nullable)
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

**Goal:** Add the worker path (Redis → Postgres), the read API, and a cleanup/hardening pass on the codebase.

#### Config (`internal/config/config.go`)

Extended `Config` with Postgres and env overrides (defaults unchanged for local dev):

| Field | Env var | Default |
|-------|---------|---------|
| `HTTPAddr` | `VANGUARD_HTTP_ADDR` | `:8080` |
| `RedisAddr` | `VANGUARD_REDIS_ADDR` | `localhost:6379` |
| `RedisListKey` | `VANGUARD_REDIS_LIST_KEY` | `vanguard:events:ingest` |
| `PostgresDSN` | `VANGUARD_POSTGRES_DSN` | `postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable` |

`Load()` uses a small `envOr()` helper — unset vars fall back to defaults.

#### Redis queue refactor (`internal/queue/redis.go`)

Merged enqueue/dequeue into a single abstraction:

- `Queue` interface — `Enqueue(ctx, []byte) error` + `Dequeue(ctx) ([]byte, error)`
- `RedisQueue` — one struct/client for both sides
- `LPUSH` on enqueue, `BRPOP` (timeout `0`) on dequeue — FIFO with Day 2's push-left pattern

#### Shared envelope (`internal/service/envelope.go`)

Single `EventEnvelope` type end-to-end (replaces Day 2's `map[string]any` + separate worker struct):

- `IngestRequest.ToEnvelope()` — sets `received_at` as `time.Time`
- `ParseEventEnvelope()` — used by the worker to unmarshal queue bytes
- `IngestService.Ingest()` — `Validate()` → `json.Marshal(ToEnvelope())` → enqueue

#### sqlc data layer

Boot.dev-style sqlc setup for Postgres access:

- `sqlc.yaml` — schema from `db/migrations/`, queries from `db/queries/`, generates into `internal/db/` with `pgx/v5`
- `db/queries/events.sql` — `CreateEvent :one` (INSERT, `status = 'pending'`) and `GetEventByID :one`
- Generated code: `internal/db/` (`Queries`, `Event`, `CreateEventParams`)

Added `github.com/jackc/pgx/v5` (via sqlc + worker). Regenerate after query changes with `make sqlc`.

#### Repository (`internal/repository/event.go`)

Thin wrapper over sqlc-generated queries:

- `EventStore` interface — `CreateEvent` + `GetEventByID`
- `PostgresEventStore` — holds `*db.Queries`, delegates to sqlc

Interface exists so services can be tested with fakes without a real Postgres.

#### Worker service (`internal/service/worker.go`)

Background consumer:

- `Worker` — holds `queue.Queue` + `repository.EventStore`
- `Run(ctx)` — loop: `Dequeue` → `processOne`; exits on `ctx.Done()` or when `Dequeue` fails on a canceled context
- `processOne` — `ParseEventEnvelope` → `store.CreateEvent`
- Malformed JSON is logged with a truncated body (`truncateForLog`, max 256 bytes) then dropped
- DB insert failures are logged (no requeue yet)

#### Read path — `GET /v1/events/{id}`

Added `GET /v1/events/{id}` to confirm events landed in Postgres:

- `EventService` (`internal/service/event.go`) — validates UUID, fetches from store, maps `db.Event` → `EventResponse`
- `GetEventHandler` (`internal/handler/get_event.go`) — 200 / 400 / 404 / 500
- `cmd/vanguard/main.go` now opens a Postgres pool (same as worker) for reads

#### HTTP routing (`internal/server/server.go`)

Moved mux registration out of `main`:

- `server.Routes` — `Ingest` + `GetEvent` handlers
- `server.New(r Routes) http.Handler` — registers `POST /v1/events` and `GET /v1/events/{id}`

#### Handler hardening (`internal/handler/`)

- `response.go` — shared `writeJSON` / `writeJSONError` (safe JSON encoding, no string concatenation)
- `ingest.go` — typed error mapping: `ValidationError` → 400, `QueueError` → 503; removed redundant POST method guard (mux handles it)
- `errors.go` (service) — `ValidationError`, `QueueError`; ingest wraps Redis failures

#### App entrypoints

**API (`cmd/vanguard/main.go`):**

```
config.Load()
  → pgxpool.New(PostgresDSN)
  → queue.NewRedisQueue
  → repository.NewPostgresEventStore
  → IngestService + EventService
  → handlers → server.New → ListenAndServe
```

**Worker (`cmd/worker/main.go`):**

```
config.Load()
  → pgxpool.New(PostgresDSN)
  → queue.NewRedisQueue
  → repository.NewPostgresEventStore
  → service.NewWorker
  → worker.Run
```

Run API and worker as two processes for the full ingest → persist → read path.

#### Makefile additions

| Target | Purpose |
|--------|---------|
| `make worker` | Run the background consumer |
| `make sqlc` | Regenerate `internal/db/` from SQL queries |

#### Tests added

| File | Covers |
|------|--------|
| `internal/service/service_test.go` | Envelope round-trip, `EventService.GetByID`, worker exits on cancel |
| `internal/handler/ingest_test.go` | Ingest HTTP contract (400/503/202), safe JSON errors |
| `internal/handler/get_event_test.go` | Get event HTTP contract |
| `internal/config/config_test.go` | Defaults and env overrides |

Removed unused `ValidateEventStatus` scaffolding (was never wired to production code).

#### End-to-end flow

```
POST /v1/events → Redis (LPUSH) → worker (BRPOP) → CreateEvent → events table
GET  /v1/events/{id} → EventService → GetEventByID → JSON response
```

#### Still deferred

- Graceful shutdown (SIGTERM, drain in-flight HTTP + worker)
- Rate limiting
- Retry + exponential backoff on DB writes
- Dead-letter queue for permanently bad events
- Requeue on transient DB failure (worker currently logs and drops)
- `Failure Modes` doc

#### Gotchas learned

- **sqlc + goose** — point `schema` at migration files; sqlc reads the DDL, goose applies it at runtime
- **`CreateEventParams.Payload` is `[]byte`** — pass `json.RawMessage` from the envelope directly; pgx maps it to JSONB
- **`received_at` needs `pgtype.Timestamptz`** — sqlc generates pgx types, not plain `time.Time`, for nullable/timestamp columns
- **One envelope struct** — marshal/unmarshal with the same `EventEnvelope`; don't hand-build JSON maps on the producer side
- **`EventStore` interface** — looks like a pass-through today, but enables fake stores in unit tests
- **Worker cancel** — check `ctx.Err()` when `Dequeue` returns an error; otherwise the loop spins after context cancel
- **Separate `cmd/worker`** — same pattern as `cmd/vanguard`; `internal/` packages are shared, binaries are not
- **FIFO** — `LPUSH` + `BRPOP` is the correct pair; do not `BRPOP` from the same end you push

#### Day 4 commands (typical flow)

```bash
make up
make migrate-up
make sqlc

# terminal 1 — API
make run

# terminal 2 — worker
make worker

# ingest
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"test","event_type":"ping","payload":{"n":1}}'

# read back (use id from Postgres)
curl -s http://localhost:8080/v1/events/<uuid>

docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id, client_id, event_type, status, received_at FROM events;"
```

---
