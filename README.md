# Vanguard

Vanguard is a small Go service that **accepts events over HTTP**, **queues them in Redis**, and **writes them to Postgres** in the background.

A `202` from the API means the event is queued, not stored yet. A separate worker process does the insert. Once it lands, you can read it back by ID.

```
POST /v1/events  →  Redis queue  →  worker  →  Postgres
GET  /v1/events/{id}  ←  Postgres
```

To get the full picture — ingest, persist, rate limits, retries, and Kubernetes — run **both** the API and the worker, not just one of them.

---

## What you need

- [Go](https://go.dev/dl/) 1.26+
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [goose](https://github.com/pressly/goose) (`go install github.com/pressly/goose/v3/cmd/goose@latest`)

Optional, for the Kubernetes path:

- [Minikube](https://minikube.sigs.k8s.io/docs/start/) and `kubectl`

---

## Run it locally

**1. Start Postgres and Redis**

```bash
make up
make migrate-up
```

If `make migrate-up` fails on WSL, a system Postgres is probably already using port 5432. Stop it (`sudo systemctl stop postgresql`), then `make down && make up && make migrate-up`.

**2. Start the API (terminal 1)**

```bash
make run
```

Listens on `http://localhost:8080`.

**3. Start the worker (terminal 2)**

```bash
make worker
```

Without this process, events sit in Redis and `GET` returns 404.

**4. Check health**

```bash
curl -s http://localhost:8080/healthz
# ok
```

`/healthz` pings both Postgres and Redis. Anything other than `ok` means a dependency is down.

---

## Send an event, then read it back

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"acme","event_type":"page_view","payload":{"url":"/home"}}'
```

You should get:

```json
{"status":"queued","id":"<uuid>"}
```

Use that `id` to read the event after the worker inserts it:

```bash
curl -s http://localhost:8080/v1/events/<uuid>
```

A short 404 after a 202 is normal — poll once or twice. The worker has to dequeue and insert first.

### Request body

| Field | Required | Meaning |
|-------|----------|---------|
| `client_id` | yes | Who sent the event |
| `event_type` | yes | What happened |
| `payload` | no | Any JSON object |

### Status codes

| Code | When |
|------|------|
| `202` | Queued in Redis; response includes `id` |
| `400` | Bad JSON, or missing `client_id` / `event_type` |
| `429` | Too many requests from this IP (rate limit) |
| `503` | Redis is down; the event was **not** queued — retry the POST |
| `404` | Worker has not inserted it yet, or the id does not exist |

---

## Unlock the rest of the system

The happy-path curl above is the start. These next pieces are what the project is built to demonstrate.

### Rate limiting

Ingest is limited per client IP with a Redis token bucket: **10 requests per second**, burst of **20**. Extra requests get `429`.

```bash
for i in $(seq 1 25); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/v1/events \
    -H 'Content-Type: application/json' \
    -d '{"client_id":"burst","event_type":"ping","payload":{"n":1}}'
done
```

You should see a mix of `202` then `429`. To turn the limiter off:

```bash
VANGUARD_RATE_LIMIT_ENABLED=false make run
```

### Load test

With API (and ideally worker) already running:

```bash
make load-test
```

Sends **200 requests/second for 30 seconds**. A high `429` count is expected with the default bucket — that is the limiter working, not a failure.

### Worker retries and the dead-letter queue

If Postgres blips, the worker retries with backoff, then puts the message back on the ingest queue. Bad JSON or a permanent DB error goes to a **dead-letter queue** (`vanguard:events:dlq`) instead of being dropped.

Inspect queues and rows:

```bash
docker compose exec redis redis-cli LLEN vanguard:events:ingest
docker compose exec redis redis-cli LLEN vanguard:events:dlq

docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id, client_id, event_type, status FROM events ORDER BY received_at DESC LIMIT 5;"
```

For what happens on each failure (outage, crash, 404 after 202), see [docs/Failure-Modes.md](docs/Failure-Modes.md).

### Graceful shutdown

`Ctrl+C` (or SIGTERM) on the API drains in-flight HTTP requests. On the worker it requeues an in-flight message so it is not lost. `kill -9` does **not** get that protection.

### Kubernetes (Minikube)

Same ingest → Redis → worker → Postgres path, with Redis, edge, and worker as pods. **Postgres stays on the host** via Docker Compose.

```bash
minikube start --cpus=4 --memory=4096
make k8s-up
kubectl get pods
minikube service edge --url
```

Use that URL in place of `http://localhost:8080`. After code changes: `make k8s-build && make k8s-deploy`.

On Linux Docker, keep `minikube service edge` running in a terminal so the tunnel stays up.

---

## Configuration

All settings are environment variables. Unset means the default.

| Variable | Default | What it does |
|----------|---------|--------------|
| `VANGUARD_HTTP_ADDR` | `:8080` | API listen address |
| `VANGUARD_REDIS_ADDR` | `localhost:6379` | Redis host |
| `VANGUARD_REDIS_LIST_KEY` | `vanguard:events:ingest` | Main queue |
| `VANGUARD_REDIS_DLQ_KEY` | `vanguard:events:dlq` | Dead-letter queue |
| `VANGUARD_POSTGRES_DSN` | `postgres://vanguard:vanguard@localhost:5432/vanguard?sslmode=disable` | Postgres |
| `VANGUARD_RATE_LIMIT_RATE` | `10` | Tokens added per second |
| `VANGUARD_RATE_LIMIT_CAPACITY` | `20` | Burst size |
| `VANGUARD_RATE_LIMIT_ENABLED` | `true` | Set `false` to skip the limiter |
| `VANGUARD_RETRY_MAX_ATTEMPTS` | `5` | Worker DB retries before requeue |
| `VANGUARD_RETRY_MAX_DELAY` | `30` | Max backoff, in seconds |

---

## Make targets

| Target | Purpose |
|--------|---------|
| `make up` / `make down` | Start / stop Postgres + Redis |
| `make migrate-up` | Apply database migrations |
| `make run` | Build and start the API |
| `make worker` | Start the background worker |
| `make test` | Run unit tests |
| `make load-test` | Hit ingest at 200 req/s |
| `make k8s-up` | Host Postgres + images + deploy to Minikube |
| `make sqlc` | Regenerate DB code after changing `db/queries/` |

---

## Layout

```
cmd/vanguard/     HTTP API
cmd/worker/       Redis → Postgres consumer
internal/         handlers, services, queue, rate limit, middleware
db/migrations/    goose SQL
k8s/              Minikube manifests (edge, worker, Redis)
scripts/          load test
```

---

## More docs

- [Failure modes](docs/Failure-Modes.md) — what breaks, what the client sees, and where the event goes
- [Dev journal](docs/dev-journal.md) — day-by-day build notes
