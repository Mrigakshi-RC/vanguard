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

<!-- Add Day 2 entry below -->
