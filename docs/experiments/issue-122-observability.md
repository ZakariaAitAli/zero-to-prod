# Issue #122 — observability experiment

- Date: 2026-10-18
- Lab: [local observability lab](../guides/local-observability-lab.md)
  (`zero-to-prod-observability-lab`, subnet `10.213.20.0/24`)
- Decision record: [ADR 0012](../adr/0012-work-items-observability.md);
  objectives: [work-items-slos.md](../reference/work-items-slos.md)
- Harness: [`tools/observability-lab-experiments`](../../tools/observability-lab-experiments)
- Evidence: [`evidence/issue-122/run2/`](../../evidence/issue-122/run2/)
  (clean run, all eight experiments passed) and
  [`evidence/issue-122/run1/`](../../evidence/issue-122/run1/) (first run,
  stopped after E4; see [its note](../../evidence/issue-122/run1/ABORTED.md))

## Question

Can an operator, using only the telemetry added for #122, tell whether users
are succeeding, find where time goes and which dependency fails, see
saturation, and be alerted on user-facing symptoms — including across the
asynchronous boundary — without reading container stdout?

## Method

Each experiment ran under synthetic load (4 iterations per second of
create item → request processing → read job, about 12 business requests per
second, unless stated), injected one fault, waited for the alert designed to
detect it, collected what an operator would use (Prometheus series and
alerts, Loki logs, Jaeger traces, and the client's own results), restored
the lab, and waited for pages to resolve before the next experiment.

Code under test: the working tree on base commit `6d10868` (PR #140 head)
with the #122 changes, built into the lab images. The run records the Git
tree of `apps/work-items` that was built (`23ba2002aa3b5f85aecb3aa92cbb4ce54a3cae4e`)
and of `infra/observability-lab` (`1d047182d2e5dec50cc0ed51c83b28de36991493`)
in [provenance.json](../../evidence/issue-122/run2/provenance.json). After the run, review added a zero-error availability fallback and
Prometheus rule tests under `infra/observability-lab`: a healthy service with
no 5xx series now reports zero bad requests instead of no data. Those rule
tests passed in the pinned Prometheus image; the long fault experiments were
not repeated for this correction. The `apps/work-items` tree differs only by
one unit test added after the run
(`TestInstrumentedStoreCountsFailureDispositions`), which is not part of the
built binaries. Docker 29.6.2,
Docker Desktop on WSL 2, 20 VM CPUs.

The alerts run on a 10 s evaluation interval with the windows from the SLO
document; nothing was shortened for the lab. Detection times therefore
depend on how much traffic and how many earlier errors the one-hour window
already held, which is discussed per experiment.

## Results (run 2)

| # | Experiment | Fault | Detected by | Time to firing | Result |
| --- | --- | --- | --- | --- | --- |
| E1 | Successful end-to-end request | none | — | — | passed |
| E2 | Resource pressure | API limited to 0.01 CPU at 8 iterations/s | `WorkItemsLatencyFastBurn` | 145 s | passed |
| E3 | Slow path: lock contention | per-item advisory lock held for 8 s | trace + error log | immediate | passed |
| E4 | PostgreSQL outage | `docker stop postgres` for 2.5 min | `WorkItemsAvailabilityFastBurn`, `WorkItemsBacklogUnobservable` | 105 s, 35 s | passed |
| E5 | API failure | `docker kill api` | `WorkItemsAPIUnavailable` | 75 s | passed |
| E6 | RabbitMQ outage | `docker stop rabbitmq` for 3.5 min | `WorkItemsAsyncProcessingStalled` | 200 s | passed |
| E7 | Worker failure | `docker stop worker` | `WorkItemsAsyncProcessingStalled` | 195 s | passed |
| E8 | Worker cannot complete (retry loop) | `REVOKE INSERT ON work_item_results` from the worker role | `WorkItemsAsyncProcessingStalled` | 200 s | passed |

Retry exhaustion: see [below](#retry-exhaustion).

### E1 — one request traced across the asynchronous boundary

150 s of load: 1,800 requests, no errors, client latency p50 4.2 ms, p95
6.5 ms, p99 7.6 ms. The sampled job's trace has 21 spans:

```
work-items-api     POST /items/{id}/process        server     3.5 ms
work-items-api       BEGIN/SELECT/INSERT/COMMIT    client     (7 query spans)
work-items-api       work_item.process publish     producer   3.5 ms
work-items-api         UPDATE postgresql           client     (mark published)
work-items-worker      work_item.process process   consumer   6.3 ms
work-items-worker        SELECT/BEGIN/.../COMMIT   client     (10 query spans)
```

The publish span is a child of the request span through the
`trace_context` stored in the outbox row, and the processing span is a child
of the publish span through the AMQP `traceparent` header. Loki returned
records from both services for the same `trace_id` (`http request`,
`processing accepted`, `delivery settled`). Acceptance-to-completion p95
was about 0.49 s, consistent with the outbox publisher's 500 ms idle poll at
this low rate.

### E2 — CPU pressure

With 0.01 CPU and 8 iterations per second (24 requests per second; the API
needs about 0.015 CPU at 12 requests per second), the latency fast burn
fired after 145 s (19.1 % of requests over 250 ms in the hour). The
telemetry showed the mechanism rather than only the symptom: API CPU pinned
at the limit, **connection-pool wait time of 18 s per second** (requests
queued for a connection because the starved process could not return
connections fast enough), p95 4.9 s, and 503s as queued requests hit the 3 s
database timeout (the availability fast burn fired too). A slow request's
trace showed a 2.4 s request in which the `INSERT` span itself took 0.6 s.
The backlog gauges failed their 1 s scrape-time query while starved
(`work_items_backlog_scrape_success 0`), which is why that series reports
failure explicitly instead of disappearing.

Run 1 had used 0.02 CPU at 4 iterations per second: the API was not
saturated, latency stayed within objective, and the alert correctly did not
fire. That run is kept as the negative control.

### E3 — lock contention (slow path)

An administrative session held the per-item advisory lock for 8 s; a
`POST /items/{id}/process` for that item returned `503
persistence_unavailable` after 3.002 s. The error log `accept processing job
failed` (`exception_message`: `timeout: context deadline exceeded`) carried
the trace ID, and the trace pinpointed the time: a `SELECT postgresql` span
of 3,000 ms (`pg_advisory_xact_lock`) marked as an error, inside a 3,000 ms
server span. After the lock was released the same request was accepted.

### E4 — PostgreSQL outage (the alert investigated end to end)

`docker stop postgres` at 14:40:07 under load.

1. **Alerts.** `WorkItemsBacklogUnobservable` fired after 35 s and
   `WorkItemsAvailabilityFastBurn` after 105 s ("15.59 % of business requests
   returned 5xx over the last hour").
2. **Symptom.** The dashboard's availability and burn-rate panels: every
   `POST /items` returned 503; the 5-minute bad ratio was 40 %.
3. **Where.** All failing requests waited the full 3 s (client p50 and p99
   3.0 s), CPU was idle, pool waits were zero: not saturation, a dependency.
4. **Which dependency.** `pg_up 0` on the dependency row; the outbox
   publisher counted `store_error` while RabbitMQ was healthy.
5. **Example.** Loki `{service_name="work-items-api"} |
   detected_level=~"warn|error"` returned `create work item failed` with
   `exception_message: insert work item: context deadline exceeded` and a
   trace ID; the trace was a single 3,000 ms server span with **no database
   child span**, which distinguishes "could not connect" from E3's "query
   blocked". This difference is now in the runbook.
6. **Recovery.** PostgreSQL started at 14:42:39; the next minute had no
   errors (672 requests) and the API reconnected without a restart. The page
   resolved 240 s
   later as the 5-minute window cleared; the slow-burn ticket stayed open, as
   designed.

Detection time depends on history: the one-hour ratio must exceed 7.2 %.
With steady traffic and no earlier errors that takes about 0.072 × 60 min ≈
4.3 min plus the 1 min `for` (run 1 measured 260 s from fault to active,
with the CPU-limited API); in run 2 the hour already held E2's errors, so
it took 105 s. A faster page for total outages would need a separate
"error ratio above 50 % for 2 minutes" rule; it was not added, to keep the
page set small, and is a candidate for #126.

### E5 — API failure

`docker kill api`: the client saw 301 connection failures, and the
server-side availability SLI recorded **no** bad requests, because a dead
API reports nothing. `WorkItemsAPIUnavailable` (scrape failure) fired after
75 s and covered the blind spot. After restart the next 480 requests
succeeded. This is the measured case for adding ingress or synthetic probes
in production.

### E6 — RabbitMQ outage

During 3.5 minutes without the broker the API stayed fully available (2,415
requests, no errors, unchanged latency): the outbox absorbed the work.
The backlog panels showed where it waited: 785 accepted jobs, all
unpublished, the oldest 196 s old; broker connection failures and worker
session restarts at 0.2 per second; the publisher logged one `outbox
publisher degraded` record (state change only, not one per retry). The
stalled-processing page fired after 200 s. After the restart the backlog
drained in 16 s. A job accepted during the outage kept a single trace:
request span, then the publish and processing spans minutes later.

The freshness fast burn did **not** fire: after the drain the one-hour late
ratio peaked at 13.9 %, just under the 14.4 % threshold, because the hour
held many on-time completions. The symptom alert on the oldest accepted job
is what detected this outage, which is why it exists alongside the burn-rate
alert.

### E7 — worker stopped

Queue depth rose to 754 ready messages with zero consumers, the worker's
scrape target went down, and the stalled-processing page fired after 195 s.
After restart the queue drained in 10 s.

### E8 — worker cannot complete (retry loop)

With `INSERT` on `work_item_results` revoked from the worker role, every
completion failed with `permission denied for table work_item_results
(SQLSTATE 42501)`, visible as `delivery handled with error` in Loki. Metrics
showed the loop: `nack_requeue` with `redelivered="true"` and session
restarts both at 1 per second, 765 ready messages, stalled-processing page
after 200 s. The stuck job's `attempt_count` stayed 0 during the fault:
infrastructure failures do not consume processing attempts, so the job was
not failed terminally. After the grant was restored it succeeded with
`attempt_count` 1 and the backlog drained.

Finding: the loop processes one delivery per second and blocks the whole
queue behind the failing message (head-of-line blocking), because the
worker leaves its session after each requeue. That is the intended
behavior for a dependency outage, and the telemetry makes it obvious, but
there is no backoff growth and no poison-message isolation. Recorded for
#126 (operations) and #123 (capacity) rather than changed here.

### Retry exhaustion

Not reproduced in the lab: the production processor
(`successfulProcessingJobProcessor`) cannot fail, and the lab did not add a
fault-injection switch to the production binary. The bounded-retry path
(three processor attempts, then `failed`) is covered by the CI integration
test `worker/retry_policy_integration_test.go`, and its telemetry by a unit
test: each recorded failure increments
`work_items_worker_processing_failures_total{disposition}`
(`retryable`, `terminal`, `already_terminal`). E8 shows the other branch:
infrastructure failures retry without consuming attempts.

## Answers to the question

| Operator question | Answered by | Shown in |
| --- | --- | --- |
| Are users succeeding? | availability, latency, freshness SLIs and burn rates; stalled-work age | E2, E4, E6–E8 |
| Where is time spent? | traces with per-query spans; latency histograms | E1, E2, E3 |
| Which dependency is failing? | `pg_up`, target `up`, publish results, backlog location (outbox / queue / accepted) | E4, E6, E7, E8 |
| Are resources saturated? | process CPU against the limit, pool wait time, in-flight requests | E2 |
| Is reliability meeting an objective? | recording rules and multi-window burn-rate alerts | all |
| Can it be investigated without stdout? | Loki logs with trace IDs linked to Jaeger | E3, E4, E8 |

## Limits

- One host, one replica of each service; Docker `stop`/`kill` and a role
  privilege change stand in for real failures. Network partitions, disk
  exhaustion, and partial broker failures were not exercised.
- Authentication was disabled and internal hops were plaintext in this lab.
- The load generator is a fixed-rate client, not the workload model #123
  will define; the SLO targets are therefore initial, as stated in the SLO
  document.
- Alerts were observed in Prometheus; no notification was delivered.
- Run 2 started with two slow-burn tickets still open from run 1; they were
  recorded in each experiment's `quiet` field and do not affect the pages
  each experiment checked.
