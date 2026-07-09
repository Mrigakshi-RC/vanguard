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
