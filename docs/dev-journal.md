# Dev journal

Day-by-day notes from building Vanguard. For how to run and use the project, see the [root README](../README.md).

---

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

### Day 5 — 2026-07-15

**Goal:** Add IP-keyed token-bucket rate limiting on `POST /v1/events` using Redis, return 429 on breach, and introduce the project's first HTTP middleware.

#### Config (`internal/config/config.go`)

Extended `Config` with rate-limit env vars:

| Field | Env var | Default |
|-------|---------|---------|
| `RateLimitRate` | `VANGUARD_RATE_LIMIT_RATE` | `10` |
| `RateLimitCapacity` | `VANGUARD_RATE_LIMIT_CAPACITY` | `20` |
| `RateLimitEnabled` | `VANGUARD_RATE_LIMIT_ENABLED` | `true` |

Added `envAsInt()` and `envAsBool()` helpers alongside the existing `envOr()`.

#### Shared Redis client (`internal/queue/redis.go`, `cmd/vanguard/main.go`, `cmd/worker/main.go`)

Refactored so each binary creates one `*redis.Client` and passes it in, instead of `NewRedisQueue` opening its own connection pool:

- `NewRedisQueue(client *redis.Client, listKey string)` — signature change from Day 4's `(addr, listKey)`
- API and worker each construct a client in `main` and share it between the queue and (on the API side) the rate limiter

#### Token-bucket limiter (`internal/ratelimit/`)

New package for Redis-backed rate limiting:

- `Limiter` — holds `*redis.Client`, `rate`, and `capacity`
- `Allow(ctx, key)` — runs an atomic Lua script via `EVAL`, returns `(allowed, retryAfter, error)`
- `token_bucket.lua` — lazy-refill token bucket stored in a Redis hash (`tokens`, `last_updated`); refills based on elapsed time, deducts 1 token per request, sets key TTL
- Script embedded with `//go:embed token_bucket.lua`

Redis key shape (built in middleware): `rate_limit:events:{clientIP}`.

#### Rate-limit middleware (`internal/middleware/ratelimit.go`)

First middleware in the project — stdlib `http.Handler` wrapping:

- `AllowLimiter` interface — `Allow(ctx, key) (bool, int, error)` so tests can stub without Redis
- `RateLimitMiddleware(limiter)` — returns a middleware function that wraps the next handler
- Extracts client IP from `r.RemoteAddr` (strips port via `net.SplitHostPort`)
- **Allowed** → passes through to the ingest handler
- **Denied** → `429 Too Many Requests` + `{"error":"Too many requests, retry after N"}`
- **Redis error** → fail open (log + allow request through)

Only `POST /v1/events` is wrapped; `GET /v1/events/{id}` is unchanged.

#### Handler export (`internal/handler/response.go`)

Renamed `writeJSONError` → `WriteJSONError` (exported) so middleware can reuse the same JSON error format as handlers.

#### App wiring (`cmd/vanguard/main.go`)

Updated dependency chain:

```
config.Load()
  → pgxpool.New(PostgresDSN)
  → redis.NewClient(RedisAddr)          # one shared client
  → queue.NewRedisQueue(client, listKey)
  → ratelimit.NewLimiter(client, cfg.RateLimitRate, cfg.RateLimitCapacity)
  → RateLimitMiddleware(limiter)(ingestHandler)   # when cfg.RateLimitEnabled
  → server.New → ListenAndServe
```

Rate limiting reads from config (`rate=10`, `capacity=20` by default). Set `VANGUARD_RATE_LIMIT_ENABLED=false` to disable.

#### Tests added

| File | Covers |
|------|--------|
| `internal/middleware/ratelimit_test.go` | Stub limiter: allow pass-through, deny → 429, Redis error → fail open |
| `internal/ratelimit/limiter_test.go` | Token bucket with miniredis: 2 allowed, 3rd denied with `retryAfter=1` |

Added `github.com/alicebob/miniredis/v2` as a test dependency for in-memory Redis (no Docker needed in unit tests).

#### End-to-end flow (ingest path)

```
POST /v1/events
  → RateLimitMiddleware (Redis token bucket, keyed by IP)
  → IngestHandler → IngestService → RedisQueue (LPUSH)
  → worker (BRPOP) → Postgres
```

#### Still deferred

- `Retry-After` as an HTTP response header (currently only in the JSON error message)
- Real client IP behind a reverse proxy (`X-Forwarded-For` / trusted proxy list)
- Load-test script (e.g. 200 req/s to confirm 429s at threshold)
- Config tests for rate-limit env var overrides
- Graceful shutdown, retry/backoff, DLQ (carried over from Day 4)

#### Gotchas learned

- **Middleware is just handler wrapping** — `func(http.Handler) http.Handler`; no framework needed
- **Rate limit before the handler** — check the bucket before JSON decode / service work
- **Lua script must be embedded** — `//go:embed` loads `token_bucket.lua` at compile time; without it `tokenBucketScript` is empty and `EVAL` fails
- **One Redis client per process** — queue and limiter share a connection pool; worker keeps its own client (separate binary)
- **`RemoteAddr` includes the port** — use `net.SplitHostPort` before using IP as the rate-limit key
- **Fail open vs fail closed** — Redis errors on the limiter allow traffic through; queue errors on ingest still return 503
- **miniredis for tests** — runs Lua `EVAL` in-process; good for unit tests without `make up`
- **Interface at the middleware boundary** — `AllowLimiter` enables stub tests; `*ratelimit.Limiter` satisfies it

#### Day 5 commands (typical flow)

```bash
make up
make migrate-up
make test

# terminal 1 — API (rate limit: 10/s, capacity 20)
make run

# terminal 2 — worker
make worker

# send requests until you hit 429
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/v1/events \
    -H 'Content-Type: application/json' \
    -d '{"client_id":"test","event_type":"ping","payload":{"n":1}}'
done

# inspect rate-limit keys in Redis
docker compose exec redis redis-cli KEYS 'rate_limit:events:*'
docker compose exec redis redis-cli HGETALL 'rate_limit:events:127.0.0.1:<port>'
```

---

### Day 6 — 2026-07-16

**Goal:** Validate rate limiting under load with a scripted benchmark, and start graceful HTTP shutdown so in-flight requests can drain on SIGTERM/SIGINT.

#### Graceful shutdown (`cmd/vanguard/main.go`)

Replaced bare `http.ListenAndServe` with an explicit `http.Server` and signal-driven shutdown:

- `server.New(r Routes)` → assigned to a mux handler, then wrapped in `&http.Server{Addr, Handler}`
- `signal.Notify` on `os.Interrupt` and `syscall.SIGTERM`
- `httpServer.Shutdown` with a 5-second context timeout after a stop signal
- Treats `http.ErrServerClosed` as a clean exit from `ListenAndServe`

Intended flow:

```
ListenAndServe (background)
  → SIGINT / SIGTERM
  → Shutdown(5s timeout)
  → drain in-flight HTTP, then exit
```

#### Load test script (`scripts/loadtest.go`)

New standalone benchmark to stress the ingest path and confirm 429s appear when the token bucket is exceeded:

| Constant | Value | Purpose |
|----------|-------|---------|
| `targetRPS` | `200` | Steady send rate via `time.Ticker` |
| `testDuration` | `30s` | Total run time (cancellable early) |
| `targetUrl` | `POST http://localhost:8080/v1/events` | Hits the rate-limited ingest route |
| `clientTimeout` | `2s` | Per-request HTTP client timeout |

Behavior:

- Fires one goroutine per tick at the target RPS
- Counts responses: `202`/`200`/`201` (allowed), `429` (rate limited), `5xx`, connection/other errors
- Supports `Ctrl+C` / SIGTERM — cancels context and prints partial results
- Reuses idle connections (`MaxIdleConnsPerHost: 100`) to avoid client-side bottlenecks

#### Makefile addition

| Target | Purpose |
|--------|---------|
| `make load-test` | Run `go run scripts/loadtest.go` against a live API |

#### End-to-end validation flow

```
make run          # start API server with rate limit (10/s, capacity 20)
make worker       # drain queue → Postgres

make load-test    # 200 req/s × 30s → expect mix of 202 and 429
```

With the Day 5 limiter settings (`rate=10`, `capacity=20`), most requests above the refill rate should return `429 Too Many Requests` once the bucket empties.

#### Still deferred

- Fix graceful-shutdown wiring — `ListenAndServe` currently blocks the main goroutine before `signal.Notify` runs; move server start to a background goroutine so SIGTERM can trigger `Shutdown` while the process is healthy
- `Retry-After` as an HTTP response header (currently only in the JSON error body)
- Real client IP behind a reverse proxy (`X-Forwarded-For` / trusted proxy list)
- Config tests for rate-limit env var overrides
- Worker graceful shutdown (API only so far)
- Retry + backoff, DLQ (carried over from Day 4)

#### Gotchas learned

- **`ListenAndServe` blocks** — signal handlers must be registered *before* or *alongside* a goroutine-started server; calling `ListenAndServe` on the main thread means shutdown code below it never runs until the server already stopped
- **Load test needs a running API** — `make load-test` does not start the server; run `make run` (and ideally `make worker`) first
- **429s are the success signal in a rate-limit test** — a high `429` count under 200 req/s confirms the middleware is doing its job, not that the system is broken
- **Variable shadowing in `main`** — `handler := server.New(...)` shadows the imported `handler` package; rename to `mux` or `srv` to avoid confusion
- **Load-test payload omits `client_id`** — requests will return `400` unless the script is updated; use a valid `IngestRequest` body to exercise the full ingest → queue path

#### Day 6 commands (typical flow)

```bash
make up
make migrate-up

# terminal 1 — API
make run

# terminal 2 — worker
make worker

# terminal 3 — load test (200 req/s, 30s)
make load-test

# quick manual burst to see 429s
for i in $(seq 1 25); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/v1/events \
    -H 'Content-Type: application/json' \
    -d '{"client_id":"load","event_type":"ping","payload":{"n":1}}'
done
```

---

### Day 7 — 2026-07-18

**Goal:** Add worker retry with exponential backoff on transient Postgres failures, requeue exhausted retries back to Redis, route permanent/malformed events to a dead-letter queue, and fix the malformed-envelope data-loss bug from Day 4.

#### Config (`internal/config/config.go`)

Extended `Config` with retry and DLQ env vars:

| Field | Env var | Default | Purpose |
|-------|---------|---------|---------|
| `RetryMaxAttempts` | `VANGUARD_RETRY_MAX_ATTEMPTS` | `5` | In-process DB insert attempts before requeue |
| `RetryBaseDelay` | `VANGUARD_RETRY_BASE_DELAY` | `1` | Intended base backoff (seconds) — see gotchas |
| `RetryMaxDelay` | `VANGUARD_RETRY_MAX_DELAY` | `30` | Max backoff cap (seconds) |
| `RedisDLQKey` | `VANGUARD_REDIS_DLQ_KEY` | `vanguard:events:dlq` | Dead-letter Redis list |

#### Queue (`internal/queue/redis.go`)

Extended the `Queue` interface and `RedisQueue` implementation:

- `Requeue(ctx, data)` — `LPUSH` back onto the main ingest list (`RedisListKey`); delegates to `Enqueue`
- `EnqueueDLQ(ctx, data)` — `LPUSH` onto the DLQ list (`RedisDLQKey`)
- `NewRedisQueue(client, listKey, dlqKey)` — signature change; both API and worker pass `cfg.RedisDLQKey`

Redis key layout:

| Key | Purpose |
|-----|---------|
| `vanguard:events:ingest` | Main queue (ingest + requeue) |
| `vanguard:events:dlq` | Dead-letter queue (permanent/malformed failures) |

#### Worker retry logic (`internal/service/worker.go`)

Replaced log-and-drop with a structured failure path in `processOne`:

1. **Malformed JSON** — log truncated body → `EnqueueDLQ` → `return` (fixes Day 4 bug where parse failure still attempted `CreateEvent`)
2. **Transient DB error** — retry up to `RetryMaxAttempts` with exponential backoff (`1s → 2s → 4s → …`, capped at `RetryMaxDelay`)
3. **Permanent DB error** — route to DLQ immediately (non-transient errors)
4. **Exhausted retries** — `Requeue` raw message back to the main Redis list
5. **Context cancelled during backoff** — requeue message so it is not lost mid-wait

`isTransientError` classifies retryable failures by substring match: `connection`, `timeout`, `deadlock`, `eof`, `refused`.

Worker log lines to watch during a Postgres outage:

```
Transient DB error: ... Retrying in 2s (Attempt 2/5)
Database insertion failed after 5 attempts: ..., requeuing to Redis
Permanent database error encountered: ... Routing to DLQ.
```

#### App wiring

Both binaries updated for the new `NewRedisQueue` signature:

- [`cmd/vanguard/main.go`](cmd/vanguard/main.go) — passes `cfg.RedisDLQKey` (API only enqueues; DLQ unused on ingest path)
- [`cmd/worker/main.go`](cmd/worker/main.go) — passes `cfg.RedisDLQKey` for worker DLQ writes

#### Tests

| File | Covers |
|------|--------|
| `internal/handler/ingest_test.go` | `stubQueue` updated with `Requeue` + `EnqueueDLQ` to satisfy extended `Queue` interface |

No dedicated worker retry unit tests yet (fake store that fails N times still deferred).

#### End-to-end flow (worker path)

```
POST /v1/events → Redis (LPUSH ingest list)
  → worker (BRPOP)
  → CreateEvent
      ├─ success → done
      ├─ transient error → backoff retry (up to N attempts)
      ├─ exhausted retries → Requeue (LPUSH ingest list)
      ├─ permanent error → EnqueueDLQ (LPUSH dlq list)
      └─ malformed JSON → EnqueueDLQ
```

#### Manual proof test — Postgres outage with zero data loss

Reset to a clean baseline, then kill Postgres mid-run:

```bash
make up
make migrate-up

# clean slate
docker compose exec postgres psql -U vanguard -d vanguard -c "TRUNCATE events;"
docker compose exec redis redis-cli DEL vanguard:events:ingest
docker compose exec redis redis-cli DEL vanguard:events:dlq

# terminal 1 — API
make run

# terminal 2 — worker
make worker

# terminal 3 — steady ingest (~5 req/s)
for i in $(seq 1 100); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/v1/events \
    -H 'Content-Type: application/json' \
    -d "{\"client_id\":\"proof\",\"event_type\":\"ping\",\"payload\":{\"n\":$i}}"
  sleep 0.2
done | tee /tmp/codes.txt

# terminal 4 — kill Postgres WHILE terminal 3 is still running
docker compose stop postgres
sleep 10
docker compose start postgres

# verify (N = number of 202 responses)
grep -c '^202$' /tmp/codes.txt
docker compose exec redis redis-cli LLEN vanguard:events:ingest   # expect 0 after drain
docker compose exec redis redis-cli LLEN vanguard:events:dlq       # expect 0
docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT count(*) FROM events;"                               # expect = N
```

**Before Day 7:** row count was less than the number of `202` responses after a mid-run outage (events popped from Redis were logged and dropped).

**After Day 7:** queue drains after Postgres recovery; `SELECT count(*)` should match the `202` count; DLQ should be empty for a clean run.

#### Still deferred

- Wire `RetryBaseDelay` into the backoff calculation (currently hardcoded `1<<retryCount` seconds)
- Inject retry config into `Worker` at construction instead of calling `config.Load()` inside `processOne`
- Worker retry unit tests (fake `EventStore` that fails N times)
- Config tests for retry/DLQ env vars
- Worker graceful shutdown (API only so far)
- Dedicated retry/DLQ consumer or admin tooling to inspect DLQ contents
- Graceful-shutdown goroutine fix (carried over from Day 6)

#### Gotchas learned

- **Requeue uses the same key as ingest** — `Requeue` is not a separate list; it `LPUSH`es back onto `vanguard:events:ingest`
- **BRPOP is destructive** — retry/requeue only helps if the message is still in memory or explicitly pushed back; the old code lost events at the first failed insert
- **Kill Postgres during ingest, not after** — stopping Postgres after a finished load test does not exercise the retry path; the outage must overlap with active worker writes
- **Clean baseline makes proof easier** — `TRUNCATE events` + `DEL` Redis keys avoids confusing old rows with the current test run
- **`make up` before `make worker`** — if Redis is down, the worker tight-loops on `Dequeue` errors and floods the terminal with `connection refused`
- **`RetryBaseDelay` config exists but is unused** — backoff currently uses `1<<retryCount` seconds regardless of the env var
- **Transient detection is string-based** — `isTransientError` matches substrings in the error message; refine as you encounter real pgx error types

#### Day 7 commands (typical flow)

```bash
make up
make migrate-up
make test

# terminal 1 — API
make run

# terminal 2 — worker
make worker

# ingest one event
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"test","event_type":"ping","payload":{"n":1}}'

# inspect queues
docker compose exec redis redis-cli LLEN vanguard:events:ingest
docker compose exec redis redis-cli LLEN vanguard:events:dlq

# confirm row landed
docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id, client_id, event_type, status FROM events ORDER BY received_at DESC LIMIT 5;"
```

---

### Day 8 — 2026-07-20

**Goal:** Harden Day 7 retry logic with bug fixes found by unit tests and the Phase 5 outage proof test, add worker retry tests, document failure behavior, and validate zero silent data loss under a mid-run Postgres outage.

#### Rate limit config alignment (`internal/config/config.go`, `cmd/vanguard/main.go`)

Aligned rate-limit defaults and wiring across the codebase:

| Setting | Was | Now |
|---------|-----|-----|
| `RateLimitCapacity` default | `100` in config, `20` hardcoded in `main` | `20` everywhere |
| `RateLimitEnabled` default | `false` in config, always on in `main` | `true`; `main` reads config and skips middleware when `false` |
| `main` wiring | `NewLimiter(redisClient, 10, 20)` | `NewLimiter(redisClient, cfg.RateLimitRate, cfg.RateLimitCapacity)` |

Added config default tests for rate (`10`), capacity (`20`), and enabled (`true`).

#### Worker bug fixes (`internal/service/worker.go`)

Three fixes on top of Day 7:

1. **Success path no longer requeues** — `break` → `return` after a successful `CreateEvent`. Previously every inserted event also fell through to the requeue block (caught by `TestWorker_retriesThenSucceeds`).

2. **Max backoff cap overflow** — replaced `maxDelay * time.Second` (double-applied duration, logged negative delays like `-1914857h...`) with `delay = min(delay, maxDelay)`.

3. **Postgres startup treated as transient** — added `"starting up"` to `isTransientError` so `57P03` errors during container recovery retry/requeue instead of landing in the DLQ.

Updated transient substrings: `connection`, `timeout`, `deadlock`, `eof`, `refused`, `starting up`.

#### Worker unit tests (`internal/service/service_test.go`)

Added fakes and three tests for `processOne`:

| Test | Proves |
|------|--------|
| `TestWorker_malformedJSONGoesToDLQ` | Bad JSON → DLQ, no `CreateEvent` call |
| `TestWorker_retriesThenSucceeds` | Transient fail twice → succeeds on 3rd attempt, no requeue |
| `TestWorker_exhaustedRetriesRequeue` | Always fails → requeue after `RetryMaxAttempts` |

Helpers: `failingEventStore` (fail first N inserts), `recordingQueue` (count requeue/DLQ calls), `validEnvelope(t)`.

#### Failure Modes doc (`docs/Failure-Modes.md`)

New internal reference (carried over from Day 4 deferred list) covering:

- Ingest path failures (400 / 429 / 503 / 202 semantics)
- Worker path (retry, requeue, DLQ, crash scenarios)
- Read path (400 / 404 / 500)
- Infrastructure failures (startup, WSL port conflict, Redis down)
- Postgres outage expected behavior + debug checklist
- Known gaps (idempotency, worker shutdown, DLQ replay)

#### Phase 5 validation — Postgres outage proof test

Ran `make load-test` (200 req/s × 30s) with `docker compose stop postgres` mid-run:

| Metric | Value |
|--------|-------|
| Accepted (`202`) | 320 |
| Rows in Postgres | 317 |
| DLQ | 3 |
| Ingest queue | 0 |

All 320 events accounted for (317 + 3) — no silent data loss. The 3 DLQ entries were from `FATAL: the database system is starting up` before the Day 8 transient fix. Worker logs during outage showed the expected retry/requeue path (`connection reset by peer` → backoff → requeue).

#### WSL Postgres port conflict (again)

`make migrate-up` failed until native Postgres was stopped:

```bash
sudo systemctl stop postgresql   # unit is postgresql, not postgres, on this WSL setup
make down && make up && make migrate-up
```

#### Still deferred

- Wire `RetryBaseDelay` into backoff (still hardcoded `1<<retryCount` seconds)
- Inject retry config into `Worker` at construction instead of `config.Load()` in `processOne`
- Config tests for retry/DLQ env vars
- Worker graceful shutdown
- DLQ replay / alerting tooling
- Graceful-shutdown goroutine fix (carried over from Day 6)

#### Gotchas learned

- **Unit tests catch real bugs** — success-path requeue was a production bug, not a test bug
- **`maxDelay * time.Second` when maxDelay is already a `Duration`** — overflows; use `min(delay, maxDelay)`
- **`postgresql` vs `postgres` systemd unit** — `systemctl stop postgres` failed on WSL; `stop postgresql` worked
- **`starting up` is transient** — Postgres returns `57P03` briefly after `docker compose start`
- **Verify all three stores** — `count(*) + LLEN dlq` should equal accepted count when debugging
- **`202` ≠ persisted** — documented explicitly in Failure Modes doc for interview prep

#### Day 8 commands (typical flow)

```bash
make up && make migrate-up
make test
go test ./internal/service/ -v -run TestWorker

# terminal 1 — API
make run

# terminal 2 — worker
make worker

# terminal 3 — load test
make load-test

# terminal 4 — kill Postgres mid-run
docker compose stop postgres && sleep 10 && docker compose start postgres

# verify
docker compose exec redis redis-cli LLEN vanguard:events:ingest
docker compose exec redis redis-cli LLEN vanguard:events:dlq
docker compose exec postgres psql -U vanguard -d vanguard -c "SELECT count(*) FROM events;"
```

---

### Day 9 — 2026-07-21

**Goal:** Make the edge API and worker container-ready for local Kubernetes (Minikube) — split multi-stage Dockerfiles, add a health probe endpoint, and clean up Redis client usage so both binaries build into small, non-root images.

#### Docker image split (`Dockerfile.edge`, `Dockerfile.worker`)

Replaced the single root `Dockerfile` (edge-only) with two explicit multi-stage builds:

| File | Builds | Entrypoint |
|------|--------|------------|
| `Dockerfile.edge` | `./cmd/vanguard` → `/usr/local/bin/vanguard` | `vanguard` |
| `Dockerfile.worker` | `./cmd/worker` → `/usr/local/bin/worker` | `worker` |

Both use the same pattern:

- **Build stage** — `golang:1.26-alpine`; copy `go.mod` / `go.sum` first for layer cache; `go mod download`; then source
- **Compile flags** — `CGO_ENABLED=0 GOOS=linux` and `-ldflags="-s -w"` (static binary, stripped symbols → smaller image)
- **Runtime stage** — `alpine:3.21`, non-root `app` user

**Why two files instead of one `Dockerfile` with build args:** independent tags (`vanguard-edge`, `vanguard-worker`), clearer Minikube/K8s manifests, and no ambiguity about which binary a plain `docker build` produces. The old root `Dockerfile` was removed to avoid drift.

#### `.dockerignore`

Added to keep build context lean:

- `.git/`, `bin/`, `vendor/`, `.env`
- `**/*_test.go` (tests not needed in production images)
- `*.md` / `README.md`

**Why:** smaller context uploads, better cache hits, and no accidental secrets from `.env`.

#### Health check (`internal/handler/health.go`, `internal/server/server.go`, `cmd/vanguard/main.go`)

Added `GET /healthz` for Kubernetes readiness/liveness probes:

- `HealthHandler` takes the same dependencies as production wiring: `*pgxpool.Pool` and `*redis.Client` from `github.com/redis/go-redis/v9`
- `Ping` Postgres and Redis; return `200` + `ok` when both are reachable, `500` with a short body when either is down
- Registered on the mux in `server.New`; wired in `main` via `handler.NewHealthHandler(dbPool, redisClient)`

**Why not import `internal/db`:** that package is sqlc-generated query code — it has no connection pool and no `Ping()`. The health handler must use the same live clients as the rest of the app.

**Why before K8s manifests:** probes need a stable HTTP endpoint; adding it now avoids deploying pods that Kubernetes thinks are ready while Redis or Postgres is unreachable.

#### Cleanup

- Removed stray `worker` binary accidentally sitting at repo root (build artifact, not source)
- Confirmed all runtime Redis usage stays on `github.com/redis/go-redis/v9 v9.21.0` (`miniredis/v2` remains test-only)

#### Still deferred

- Kubernetes manifests under `k8s/` (edge, worker, Redis Deployments/Services/ConfigMaps)
- Minikube workflow (`eval $(minikube docker-env)`, `imagePullPolicy: Never`, Postgres on host via `host.minikube.internal`)
- Makefile targets: `k8s-build`, `k8s-deploy`, `k8s-up`
- Readiness probe semantics — consider `503` instead of `500` when deps are down (K8s convention)
- Root `Dockerfile` alias for `docker build .` without `-f` (optional convenience)
- Graceful-shutdown goroutine fix (carried over from Day 6)

#### Gotchas learned

- **`go build` flag order** — `-ldflags` must come **before** the package path (`go build -ldflags="..." -o out ./cmd/...`), not after
- **Worker Dockerfile copy-paste** — easy to leave `./cmd/vanguard` and `ENTRYPOINT ["vanguard"]` in the worker file; build and run both images locally before pushing to K8s
- **`internal/db` ≠ database connection** — sqlc's `db` package name collides mentally with "the DB"; health checks need `pgxpool` + `redis.Client`
- **Two binaries ⇒ two images** — compose today only runs Postgres + Redis; the app processes still run on the host until K8s manifests land

#### Day 9 commands (typical flow)

```bash
make up && make migrate-up

# Build edge image
docker build -f Dockerfile.edge -t vanguard-edge:latest .

# Build worker image
docker build -f Dockerfile.worker -t vanguard-worker:latest .

# Run API locally and probe health
make run
curl -s http://localhost:8080/healthz    # expect: ok

# Optional — smoke-test edge container against compose infra
docker run --rm --network host \
  -e VANGUARD_REDIS_ADDR=localhost:6379 \
  -e VANGUARD_POSTGRES_DSN='postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable' \
  vanguard-edge:latest
```

---

### Day 10 — 2026-07-26

**Goal:** Deploy edge, worker, and Redis to Minikube with plain Kubernetes manifests — same ingest → queue → worker → Postgres behavior as compose + `make run` / `make worker`, with Postgres staying on the host.

#### Architecture on Minikube

```
POST /v1/events  →  edge Pod  →  Redis Service (redis:6379)
                              ↘  host.minikube.internal:5432 (Postgres via compose)

worker Pod  →  BRPOP redis  →  INSERT Postgres (host)
GET /v1/events/{id}  →  edge Pod  →  Postgres (host)
```

| Component | Where it runs | Why |
|-----------|---------------|-----|
| **Postgres** | Host — `docker compose up postgres -d` | DB off-cluster (local dev); matches “don’t run Postgres in K8s” practice |
| **Redis** | `k8s/redis/` | Queue + rate-limit cache; ephemeral, same role as compose Redis |
| **Edge** | `k8s/edge/` | HTTP API with probes on `/healthz` |
| **Worker** | `k8s/worker/` | Background consumer; no Service (nothing calls it inbound) |

#### Kubernetes manifests (`k8s/`)

```
k8s/
├── configmap.yaml           # shared VANGUARD_* env vars
├── redis/
│   ├── deployment.yaml      # redis:7, 1 replica
│   └── service.yaml         # ClusterIP redis:6379
├── edge/
│   ├── deployment.yaml      # vanguard-edge:latest, probes, init container
│   └── service.yaml         # NodePort 8080 (fixed nodePort 30080)
└── worker/
    └── deployment.yaml      # vanguard-worker:latest, init container
```

**ConfigMap (`vanguard-config`)** — mirrors [`internal/config/config.go`](internal/config/config.go):

- `VANGUARD_REDIS_ADDR=redis:6379` — K8s Service DNS (not `localhost`)
- `VANGUARD_POSTGRES_DSN=...@host.minikube.internal:5432/...` — reach host Postgres from pods
- All numeric/boolean values **quoted** (`"10"`, `"true"`) — ConfigMap `data` values must be strings
- `apiVersion: v1` — ConfigMaps are core `v1`, not `apps/v1`

**Edge Deployment:**

- `imagePullPolicy: Never` — images loaded into Minikube with `minikube image load` (no registry)
- `envFrom.configMapRef` — inject config without rebuilding images
- **Init container** — `busybox` waits for `redis:6379` before main container starts
- **readinessProbe / livenessProbe** — `GET /healthz:8080` (Day 9 handler)

**Edge Service** — `NodePort` with `nodePort: 30080` for stable local access; `minikube service edge --url` for tunnel URL on Linux Docker driver.

**Worker Deployment** — same ConfigMap + Redis init container; no Service, no HTTP probes (no HTTP server).

#### Makefile targets (`Makefile`)

| Target | Steps |
|--------|--------|
| `make k8s-build` | Build edge + worker with `DOCKER_CONFIG=~/.docker-nocreds` (WSL credential-helper workaround) → `minikube image load` both tags |
| `make k8s-deploy` | `kubectl apply` ConfigMap, then redis → edge → worker (ConfigMap first so env exists) |
| `make k8s-up` | `docker compose up postgres -d` → `migrate-up` → `k8s-build` → `k8s-deploy` |

**Prerequisite:** `minikube start` before `make k8s-up`.

#### Image build strategy (WSL + Docker Desktop)

Building inside `eval $(minikube docker-env)` hit `docker-credential-desktop.exe: exec format error`. Working path:

1. Build on **host** Docker with empty cred config: `DOCKER_CONFIG=~/.docker-nocreds`
2. Load into cluster: `minikube image load vanguard-edge:latest` (and worker)

No GHCR push needed for local Minikube.

#### End-to-end validation

Milestone reached when:

```bash
kubectl get pods
# edge, redis, worker all 1/1 Running

curl -s http://127.0.0.1:<minikube-service-port>/healthz   # ok

curl -s -X POST http://127.0.0.1:<port>/v1/events \
  -H "Content-Type: application/json" \
  -d '{"client_id":"k8s-test","event_type":"test","payload":{"foo":"bar"}}'
# {"status":"queued"}

# ID is assigned by Postgres on insert, not returned in 202 — look up via psql:
docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id FROM events WHERE client_id = 'k8s-test' ORDER BY received_at DESC LIMIT 1;"

curl -s http://127.0.0.1:<port>/v1/events/<uuid>
```

POST `202` + GET persisted event proves edge → Redis → worker → host Postgres → read API on K8s.

#### Still deferred

- HPA / autoscaling, Ingress, Helm/Kustomize
- Kubernetes Secrets for Postgres password (ConfigMap OK for local dev only)
- Return event `id` in `202` ingest response (today only `{"status":"queued"}`)
- `503` on `/healthz` when deps down (K8s convention vs current `500`)
- Probe `initialDelaySeconds` / `periodSeconds` (commented out in edge deployment)
- `k8s-down` Makefile target
- Graceful-shutdown goroutine fix (carried over from Day 6)

#### Gotchas learned

- **ConfigMap YAML** — `apiVersion: v1` (not `apps/v1`); quote all values (`"10"`, not `10`; `"true"`, not `true`)
- **Deployment YAML** — `apiVersion: apps/v1`; Service `ports` must be a list (`- port: 6379`)
- **Init container typo** — `- name:` needs a space after `-`; bad indentation breaks `kubectl apply`
- **Apply order** — ConfigMap before edge/worker; otherwise pods stall waiting for `vanguard-config`
- **`docker-credential-desktop.exe` on WSL** — breaks `docker build` even with `minikube docker-env -u`; use `DOCKER_CONFIG=~/.docker-nocreds` with `{}` config
- **`minikube docker-env` vs `minikube image load`** — eval points CLI at Minikube’s daemon; host build + `image load` avoids Minikube DNS/credential issues during `go mod download`
- **`minikube ssh` ≠ host compose** — run `docker compose up postgres` on WSL, not inside `minikube ssh`
- **`host.minikube.internal`** — pods reach host Postgres; verify with `minikube ssh -- nc -vz host.minikube.internal 5432`
- **WSL native Postgres on 5432** — `make migrate-up` hits wrong server until `sudo systemctl stop postgresql` and compose Postgres is sole listener
- **`minikube service edge` on Linux Docker driver** — tunnel needs terminal open; use URL from `--url` without double `http://` in curl
- **Ingest response has no id** — worker/DB assign UUID; use `psql` to GET test until API returns id in `202`

#### Day 10 commands (typical flow)

```bash
minikube start --cpus=4 --memory=4096

make k8s-up

kubectl get pods
minikube service edge --url

# terminal 1 (optional — keeps tunnel alive on Linux)
minikube service edge

# terminal 2
curl -s http://127.0.0.1:<port>/healthz
curl -s -X POST http://127.0.0.1:<port>/v1/events \
  -H "Content-Type: application/json" \
  -d '{"client_id":"k8s-test","event_type":"test","payload":{"foo":"bar"}}'

docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id FROM events WHERE client_id = 'k8s-test' ORDER BY received_at DESC LIMIT 1;"

curl -s http://127.0.0.1:<port>/v1/events/<uuid>

# rebuild after code changes
make k8s-build && make k8s-deploy
```

---

### Day 11 — 2026-08-23

**Goal:** Fix graceful shutdown on both binaries — edge API and worker — so SIGINT/SIGTERM drain or requeue in-flight work instead of dropping it. Update ops docs to match.

#### Edge API graceful shutdown (`cmd/vanguard/main.go`)

Closed the Day 6 wiring bug: `ListenAndServe` had been blocking `main`, so the signal handler and `http.Server.Shutdown` never ran.

Changes:

- `ListenAndServe` moved to a **background goroutine**
- Main thread blocks on `<-stop` after `signal.Notify(os.Interrupt, syscall.SIGTERM)`
- `httpServer.Shutdown` with 5-second timeout drains in-flight HTTP
- `http.ErrServerClosed` treated as clean exit in the server goroutine
- `defer redisClient.Close()` and `defer dbPool.Close()` run after shutdown completes

Intended flow (now actually reachable):

```
go ListenAndServe()
  → SIGINT / SIGTERM on main
  → Shutdown(5s)
  → close Redis + Postgres pool
  → exit
```

#### Worker graceful shutdown (`cmd/worker/main.go`, `internal/service/worker.go`)

Added shutdown parity with the edge API:

**`cmd/worker/main.go`**

- `context.WithCancel` passed into `worker.Run`
- Worker runs in a goroutine with `sync.WaitGroup`
- SIGINT/SIGTERM → `cancel()` → `wg.Wait()` before `main` returns (pool stays open until worker exits)

**`internal/service/worker.go` — Option A (`handedOff` + defer)**

- `processOne` tracks whether the message fate is decided (`handedOff`)
- Defer on exit: if context cancelled and not handed off → requeue to ingest list (`context.Background()` so requeue isn't blocked by cancelled ctx)
- `handedOff = true` on: successful insert, successful explicit requeue (exhausted retries), DLQ routing
- Backoff cancel during shutdown: return early; defer handles requeue (removed duplicate explicit requeue in the backoff branch)

**`internal/queue/redis.go` — cancellable dequeue**

- Replaced blocking `BRPOP(ctx, 0, ...)` with a **1-second timeout loop**
- On timeout (`redis.Nil`), loop and re-check `ctx.Err()` so shutdown isn't stuck waiting for the next message

Worker shutdown flow:

```
go worker.Run(ctx)
  → SIGINT / SIGTERM
  → cancel()
  → Dequeue returns ctx.Err() OR processOne defer requeues in-flight message
  → wg.Wait()
  → exit
```

#### Docs (`docs/Failure-Modes.md`)

Updated to reflect both shutdown paths:

- Infrastructure table: edge and worker SIGTERM behavior documented
- Worker path table: graceful shutdown row added; SIGKILL/panic still called out as data-loss case
- Known gaps: removed "Worker has no graceful shutdown" and "API graceful shutdown unreachable"

#### End-to-end validation

**Edge:**

```bash
make up && make migrate-up
make run
# Ctrl+C → expect "Shutting down server gracefully..." then "Server gracefully stopped..."
```

**Worker:**

```bash
make worker
# POST an event, Ctrl+C during processing
# expect "Shutting down worker..." then "Worker shutdown complete"
# verify message not lost:
docker compose exec redis redis-cli LLEN vanguard:events:ingest
```

**K8s:** edge deployment already sends SIGTERM on pod delete; worker pods now requeue in-flight messages instead of losing them on rollout (still subject to `terminationGracePeriodSeconds`).

#### Still deferred

- Return event `id` in `202` ingest response
- Idempotency keys (shutdown requeue + requeue path can still duplicate rows without them)
- DLQ failure sets `handedOff = true` even when `EnqueueDLQ` fails — message may be lost unless shutdown defer requeues
- `Retry-After` HTTP header, `X-Forwarded-For` / trusted proxy for rate limit
- Inject retry config into `Worker` at construction (still calls `config.Load()` inside `processOne`)
- Worker shutdown unit tests (cancel mid-`processOne`)
- CI/CD, DLQ replay tooling (carried over from earlier days)

#### Gotchas learned

- **`ListenAndServe` on main blocks forever** — shutdown code after it is dead until you move the server to a goroutine (Day 6 intent, Day 11 fix)
- **`wg.Wait()` matters on the worker** — calling `cancel()` without waiting lets `main` close the Postgres pool while `processOne` is still running
- **`handedOff` prevents double requeue** — set it on insert success, not only on explicit requeue; otherwise shutdown defer can requeue an already-persisted event
- **`handedOff` on DLQ failure is still loose** — if `sendToDLQ` fails, code still marks handed off; defer won't retry (minor edge case)
- **`BRPOP` with timeout 0 blocks shutdown** — 1s polling loop lets `ctx.Done()` be checked between waits
- **SIGKILL vs SIGTERM** — graceful path only applies to SIGINT/SIGTERM; `kill -9` still loses in-flight worker messages
- **Defer requeues to ingest, not DLQ** — malformed messages that fail DLQ during shutdown may loop until DLQ succeeds

#### Day 11 commands (typical flow)

```bash
make up
make migrate-up

# terminal 1
make run
curl -s http://localhost:8080/healthz

# terminal 2
make worker
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"shutdown-test","event_type":"ping","payload":{"n":1}}'

# Ctrl+C worker, then edge — confirm shutdown logs on each
make test
```

---

### Day 12 — 2026-08-24

**Goal:** Assign event IDs at ingest time and return them in the `202` response, so clients can poll `GET /v1/events/{id}` without a Postgres lookup — and the worker inserts the same UUID (foundation for future idempotency).

#### Ingest path — server-generated UUID (`internal/service/envelope.go`, `internal/service/ingest.go`)

- Added `github.com/google/uuid` dependency
- `EventEnvelope` gains `id` field; `ToEnvelope()` sets `uuid.NewString()` when the API accepts a request
- `IngestService.Ingest` now returns `(string, error)` — the envelope ID after successful `LPUSH`
- Handler `202` body changed from `{"status":"queued"}` to `{"status":"queued","id":"<uuid>"}`

Flow:

```
POST /v1/events
  → validate request
  → ToEnvelope() assigns UUID
  → LPUSH envelope JSON to Redis
  → 202 { status, id }
```

**Why at ingest, not at insert:** client gets a stable correlation ID immediately; same ID travels through the queue and into Postgres.

#### Worker path — use envelope ID on insert (`internal/service/worker.go`, `db/queries/events.sql`)

- `CreateEvent` SQL now accepts explicit `id` (no longer relies solely on `gen_random_uuid()` default)
- Worker parses `env.ID` with `pgtype.UUID.Scan`; invalid/missing UUID → DLQ (same as malformed JSON)
- Insert uses envelope UUID so `GET /v1/events/{id}` matches the `202` response after worker runs

#### Docs (`docs/Failure-Modes.md`)

- Ingest table: `202` documents returned `id`; note to poll GET after worker persists
- Worker table: invalid envelope `id` → DLQ; successful insert uses same UUID as `202`
- Read path: clarify `404` until worker inserts despite having `id` from POST
- DLQ section: missing/invalid UUID listed as DLQ reason

#### Tests

| Test | Proves |
|------|--------|
| `TestEventEnvelope_roundTrip` | Envelope JSON includes valid UUID |
| `TestIngestService_returnsEnqueuedID` | Returned ID matches queued envelope ID |
| `TestIngestHandler` (extended) | `202` response includes parseable UUID |
| `TestWorker_invalidEnvelopeIDGoesToDLQ` | Bad envelope `id` → DLQ, no `CreateEvent` |

Helpers: `captureQueue` records enqueued bytes for ingest assertions.

#### End-to-end validation

```bash
make up && make migrate-up
make run    # terminal 1
make worker # terminal 2

# ingest — capture id from 202
curl -s -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -d '{"client_id":"id-test","event_type":"ping","payload":{"n":1}}'
# {"status":"queued","id":"550e8400-..."}

curl -s http://localhost:8080/v1/events/<id-from-response>
```

On K8s (after rebuild): same flow against `minikube service edge --url` — no `psql` lookup needed for the happy path.

#### Still deferred

- True idempotency (dedupe on `id` / unique constraint conflict handling on retry-requeue)
- Return `id` in load-test script assertions
- Regenerate sqlc in CI if not already wired
- HPA, Ingress, Secrets (carried over from Day 10)
- Graceful-shutdown DLQ-failure edge case (Day 11)

#### Gotchas learned

- **`202` without `id` was a K8s testing papercut** — Day 10 required `psql` to GET; server-assigned ID at ingest fixes the client loop
- **UUID must live on the envelope** — worker only sees Redis bytes; ID has to be in JSON before `LPUSH`
- **Invalid UUID → DLQ, not retry** — treat like malformed envelope; don’t hammer Postgres with bad `pgtype.UUID`
- **`CreateEvent` signature change** — sqlc regen required after adding `id` to INSERT; worker must pass `pgtype.UUID`
- **`404` after `202` is still normal briefly** — GET hits Postgres; worker may not have inserted yet

#### Day 12 commands (typical flow)

```bash
make up && make migrate-up
make sqlc          # if queries changed locally
make test

make run
make worker

curl -s -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -d '{"client_id":"day12","event_type":"test","payload":{"ok":true}}'

# use id from JSON response
curl -s http://localhost:8080/v1/events/<id>
```

---
