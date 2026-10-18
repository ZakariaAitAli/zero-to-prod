# Runbook: Work Items alerts

Each Work Items alert links to a section here (`runbook` annotation). The
SLOs and the reasoning behind each alert are in
[work-items-slos.md](../reference/work-items-slos.md); the tools are
described in the [observability lab guide](../guides/local-observability-lab.md).

Start every investigation on the Grafana dashboard **Work Items service
health**: the top row says which user-facing objective is hurting, the rows
below say where.

General method:

1. **Confirm the symptom** on the top row (which SLI, since when).
2. **Find the failing component** with the RED rows (API) and the
   asynchronous row (backlog location: outbox, queue, or accepted).
3. **Find the cause** with the dependency row (target `up`, PostgreSQL,
   connection-pool waits, CPU, memory).
4. **Get an example**: the logs panel (or Grafana Explore → Loki) shows
   warnings and errors with `trace_id`; open the trace in Jaeger to see which
   span took the time or failed. Useful Loki queries:
   - `{service_name="work-items-api"} | detected_level=~"warn|error"`
   - `{service_name="work-items-worker"} | job_id="123"`
   - `{service_name=~"work-items-.*"} | trace_id="<id>"`
5. Mitigate, then watch the short-window burn rate fall and the alert
   resolve.

## availability-burn

`WorkItemsAvailabilityFastBurn` (page), `WorkItemsAvailabilitySlowBurn` (ticket).

Business requests are returning 5xx.

- **5xx ratio by route**: all routes failing points at a shared dependency;
  one route points at its own code path.
- Error logs: `create work item failed`, `accept processing job failed`,
  `get processing job failed` carry the error in `exception_message`.
  `context deadline exceeded` appears in two different situations; tell them
  apart with the trace and `pg_up`:
  - **PostgreSQL unreachable** (observed when its container was stopped):
    every request waits the full 3 s, the trace has *no* database child spans
    (no connection was obtained), and `pg_up` is 0.
  - **A slow or blocked query**: the trace has a database span that ran for
    about 3 s and is marked as an error; `pg_up` is 1. See
    [latency-burn](#latency-burn).
- `WorkItemsBacklogUnobservable` fires on the same cause and usually first,
  because the backlog query fails on the next scrape.
- Mitigation: restore PostgreSQL (restart the container, free disk, restore
  per the [recovery runbook](work-items-recovery.md)). The API needs no
  restart: it reconnects on the next request.

## latency-burn

`WorkItemsLatencyFastBurn` (page), `WorkItemsLatencySlowBurn` (ticket).

More than the allowed share of business requests take longer than 250 ms.

- Latency panel: is the whole distribution slower (CPU, pool) or only the
  tail (locks, a few slow queries)?
- **In-flight requests and API CPU**: in-flight rising with CPU flat means
  waiting on a dependency; CPU at its limit means CPU starvation (check
  container limits and host load).
- **Connection pools**: waiting time above zero means the pool is exhausted.
- Open a slow request's trace (logs: `route="/items" | duration_ms > 250`).
  A long `SELECT postgresql` span on `pg_advisory_xact_lock` is a per-item
  lock wait: look for a long-open transaction
  (`pg_stat_activity_max_tx_duration`, `SELECT * FROM pg_stat_activity
  WHERE state = 'idle in transaction'`).
- Mitigation: end the blocking session, remove the CPU constraint, or
  reduce load; capacity work is #123.

## async-stalled

`WorkItemsAsyncProcessingStalled` (page), `WorkItemsFreshnessFastBurn` (page).

Accepted work is not finishing, or finishing late. Look at **Backlog** to
see where the work is waiting:

| Where | Signals | Likely cause |
| --- | --- | --- |
| Outbox (unpublished rising) | `outbox publisher degraded state=broker_unavailable` log, broker connect failures, `up{job="rabbitmq"} == 0` | RabbitMQ down or unreachable |
| Queue (ready rising, consumers 0) | `up{job="work-items-worker"} == 0`, no `delivery settled` logs | worker stopped or crashing |
| Accepted but neither (queue small, deliveries `nack_requeue` with `redelivered=true`, session restarts) | worker logs `delivery handled with error` with an `exception_message` (for example SQLSTATE 42501, permission denied) | worker cannot finish: database permission, schema, or data problem |

- A requeue loop does not consume processing attempts (infrastructure
  failures are not processor failures), so it continues until the cause is
  fixed; it is visible as one redelivery per second.
- Mitigation: restore the broker or worker, or fix the database cause. The
  outbox and queue are durable; the backlog drains by itself afterwards
  (watch **Oldest waiting work** fall). Jobs accepted during the outage keep
  their trace: the publish and processing spans join the original request's
  trace, so the trace duration shows the delay.
- If a single job stays stuck while others complete, inspect it with
  `GET /processing-jobs/{id}` and the worker logs for its `job_id`.

## api-unavailable

`WorkItemsAPIUnavailable` (page).

Prometheus cannot scrape the API, so the request SLIs are blind. Assume
users are failing.

- `docker compose ps` / orchestrator status for the API; restart count;
  last logs before the stop (`docker logs` or Loki
  `{service_name="work-items-api"}`; the OTLP pipeline loses only the
  records still buffered at the crash).
- If the API is running but not scrapeable, check `METRICS_ADDRESS` and the
  network between Prometheus and the API.
- Mitigation: restart the API. Accepted work is safe in the outbox and
  resumes publishing on start.

## backlog-unobservable

`WorkItemsBacklogUnobservable` (ticket).

The API cannot read the backlog gauges from PostgreSQL, so
`WorkItemsAsyncProcessingStalled` cannot fire. Usually it accompanies
[availability-burn](#availability-burn) (PostgreSQL down). If it fires
alone, check the API role's privileges on `outbox_messages` and
`processing_jobs` (`created_at`, `published_at`, `state`).
