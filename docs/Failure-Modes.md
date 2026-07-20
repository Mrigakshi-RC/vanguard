# Failure Modes

What can go wrong in Vanguard, and what the system does today. Useful for ops debugging and interview walkthroughs.

**Architecture reminder:** `POST /v1/events` → Redis ingest list → worker → Postgres. Read path hits Postgres directly.

---

## Ingest path (`POST /v1/events`)

| Failure | What happens | Client sees | Data fate |
|---------|--------------|-------------|-----------|
| Invalid JSON body | Handler rejects before service | `400` — `"invalid JSON body"` | Never queued |
| Missing `client_id` or `event_type` | Validation fails in `IngestService` | `400` — field error | Never queued |
| Rate limit exceeded (Redis token bucket) | Middleware blocks before handler | `429` — `"Too many requests, retry after N"` | Never queued |
| Redis down during rate-limit check | Middleware **fail open** — logs error, passes request through | Normal ingest flow (or downstream failure) | Depends on queue |
| Redis down during enqueue | `LPUSH` fails → `QueueError` | `503` — `"service temporarily unavailable"` | **Lost** — client should retry POST |
| Enqueue succeeds | Event JSON on `vanguard:events:ingest` | `202` — `{"status":"queued"}` | Safe in Redis until worker pops it |

**Important:** `202 Accepted` means queued in Redis, **not** persisted in Postgres yet.

---

## Worker path (Redis → Postgres)

| Failure | What happens | Logs | Data fate |
|---------|--------------|------|-----------|
| Malformed queue message (bad JSON) | Routed to DLQ (`vanguard:events:dlq`) | `Malformed event envelope: ...` | In DLQ — not in Postgres |
| Transient Postgres error (connection reset, refused, timeout, deadlock, `starting up`, etc.) | In-process retry with exponential backoff (`1s → 2s → 4s …`, max 30s), up to 5 attempts | `Transient DB error: ... Retrying in ...` | Still in worker memory during retries |
| Transient error, retries exhausted | Message requeued to ingest list | `Database insertion failed after 5 attempts: ..., requeuing to Redis` | Back on `vanguard:events:ingest` — worker will retry later |
| Permanent Postgres error (constraint violation, auth failure, etc.) | Routed to DLQ immediately — no retry | `Permanent database error encountered: ... Routing to DLQ.` | In DLQ — not in Postgres |
| Insert succeeds | Worker returns — done | (none) | Row in `events` table |
| Redis down during `Requeue` / `EnqueueDLQ` | Error logged; message may be **lost** (already popped by `BRPOP`) | `Failed to requeue...` / `Failed to send ... to DLQ` | **At risk** — was in memory only |
| Worker not running | Events accumulate in Redis ingest list | Clients still get `202` | Safe in Redis until worker starts |
| Worker crash mid-`processOne` | In-flight message lost if not yet requeued | Process gone | **Lost** if popped but not inserted/requeued |

**Delivery guarantee today:** at-least-once for happy paths with retry + requeue; at-most-once if worker dies or Redis write fails after dequeue.

---

## Read path (`GET /v1/events/{id}`)

| Failure | What happens | Client sees |
|---------|--------------|-------------|
| Missing or empty ID | Handler validation | `400` — `"event id is required"` |
| Invalid UUID format | `EventService` rejects | `400` — `"invalid event id"` |
| Event not in Postgres | `pgx.ErrNoRows` | `404` — `"event not found"` |
| Postgres down / query error | Wrapped DB error | `500` — `"internal server error"` |

Read path does **not** check Redis. An event can be queued (`202`) but return `404` until the worker inserts it.

---

## Infrastructure failures

| Failure | API (`cmd/vanguard`) | Worker (`cmd/worker`) |
|---------|----------------------|------------------------|
| Postgres unreachable at startup | `log.Fatalf` — process exits | `log.Fatalf` — process exits |
| Redis unreachable at startup | Starts (no startup ping) | Starts, then tight-loops on `Dequeue` errors |
| Postgres down during runtime | Ingest still works (Redis only); GET returns `500` | Retry → requeue or DLQ (see worker table) |
| Redis down during runtime | Ingest returns `503`; rate limit fail-open | `Dequeue` errors logged; loop spins |
| Wrong Postgres on `localhost:5432` (WSL port conflict) | May connect to wrong server — auth errors | Permanent errors → **DLQ** (not retried) |
| API killed (SIGINT/SIGTERM) | Graceful shutdown intended (5s drain) — wiring incomplete | N/A |
| Worker killed (SIGINT/SIGTERM) | N/A | No graceful shutdown — in-flight work may be lost |

---

## Dead-letter queue (DLQ)

**Key:** `vanguard:events:dlq`

Messages land here when:

- JSON envelope cannot be parsed
- Postgres returns a **non-transient** error
- (Previously) Postgres `starting up` before Day 8 fix — now retried

**There is no DLQ consumer.** Messages sit in Redis until manually inspected or deleted:

```bash
docker compose exec redis redis-cli LLEN vanguard:events:dlq
docker compose exec redis redis-cli LRANGE vanguard:events:dlq 0 -1
```

---

## Postgres outage (expected resilient behavior)

When Docker Postgres is stopped mid-ingest:

1. API keeps accepting events → Redis ingest list **grows**
2. Worker logs transient errors, retries, then **requeues**
3. After Postgres restarts, worker drains the queue → rows appear in `events`
4. Verify: `accepted_202_count == SELECT count(*) FROM events` (DLQ should be 0)

If counts don't match, check DLQ length and worker logs for `Permanent database error` or `Routing to DLQ`.

---

## Known gaps (not handled yet)

| Gap | Risk |
|-----|------|
| No idempotency key on events | Requeue could duplicate rows if insert succeeded but worker didn't notice |
| Worker has no graceful shutdown | SIGKILL mid-backoff may lose in-flight messages |
| `RetryBaseDelay` config unused | Backoff hardcoded to `2^attempt` seconds |
| Rate limit always on in dev | Defaults: 10 tokens/s refill, burst capacity 20 (`VANGUARD_RATE_LIMIT_*`) |
| No DLQ replay / alerting | Poison or misclassified messages sit silently in Redis |
| No Redis persistence guarantee beyond Docker volume | Container wipe loses queued events |
| API graceful shutdown code unreachable | `ListenAndServe` blocks main goroutine before signal handler |

---

## Quick debug checklist

```bash
# Queue depth
docker compose exec redis redis-cli LLEN vanguard:events:ingest
docker compose exec redis redis-cli LLEN vanguard:events:dlq

# Row count
docker compose exec postgres psql -U vanguard -d vanguard -c "SELECT count(*) FROM events;"

# Recent rows
docker compose exec postgres psql -U vanguard -d vanguard \
  -c "SELECT id, client_id, event_type, status FROM events ORDER BY received_at DESC LIMIT 5;"
```

**Rule of thumb:** If clients got `202` but rows are missing, check ingest queue → worker logs → DLQ — in that order.
