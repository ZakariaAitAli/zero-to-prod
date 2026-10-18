# Local observability lab

Issue #122, [ADR 0012](../adr/0012-work-items-observability.md). An opt-in
Docker Compose lab that runs the Work Items API and worker with their
dependencies and a complete telemetry pipeline, for learning and for the
failure experiments in
[issue-122-observability.md](../experiments/issue-122-observability.md).

```
API, worker ──OTLP/HTTP──▶ OpenTelemetry Collector ──▶ Jaeger (traces)
                                                   └──▶ Loki (logs)
Prometheus ──scrape──▶ API, worker, RabbitMQ, postgres-exporter, collector, Loki
Grafana ──▶ Prometheus, Loki, Jaeger (linked by trace ID)
```

## Start, use, stop

```bash
tools/observability-lab-local up
```

`up` creates the private state directory
(`~/.local/state/zero-to-prod/observability-lab`, mode 700) with random
passwords and connection URLs, checks that the subnet (`10.213.20.0/24`) is
free, builds the API and worker images from the working tree, applies the
migrations, and starts everything. Published ports bind to `127.0.0.1` only:

| What | URL |
| --- | --- |
| API (authentication disabled) | http://127.0.0.1:18080 |
| Grafana (anonymous read-only) | http://127.0.0.1:13000, dashboard *Work Items / Work Items service health* |
| Prometheus (alerts, rules, queries) | http://127.0.0.1:19090/alerts |
| Jaeger | http://127.0.0.1:16686 |

Drive traffic and run the failure experiments:

```bash
tools/observability-lab-local load 120 4 /tmp/load.csv
```

```bash
tools/observability-lab-experiments evidence/issue-122/<new-run-name>
```

Stop without losing data (volumes, secrets, and the network are kept):

```bash
tools/observability-lab-local stop
```

Ports, the subnet, and the state directory can be changed with the
`ZTP_OBSERVABILITY_LAB_*` variables listed at the top of the script.

## What is collected

**Metrics** (Prometheus, pulled every 10 s):

| Area | Metrics | Question answered |
| --- | --- | --- |
| HTTP (RED) | `work_items_http_requests_total{route,method,code}`, `work_items_http_request_duration_seconds`, `work_items_http_requests_in_flight` | rate, errors, duration per route |
| Async backlog | `work_items_processing_jobs_accepted`, `work_items_processing_job_oldest_accepted_age_seconds`, `work_items_outbox_unpublished_messages`, `work_items_outbox_oldest_unpublished_age_seconds`, `work_items_backlog_scrape_success` | is accepted work waiting, where, and for how long |
| Outbox publisher | `work_items_outbox_publish_total{result}`, `_publish_duration_seconds`, `_publish_lag_seconds`, `_broker_connect_failures_total` | is the broker accepting messages |
| Worker | `work_items_worker_deliveries_total{settlement,redelivered}`, `_delivery_duration_seconds`, `_job_completions_total{disposition}`, `_job_completion_latency_seconds`, `_processing_failures_total{disposition}`, `_session_exits_total` | throughput, retries, duplicates, processing time |
| Connection pools (USE) | `work_items_db_pool_*` (in use, idle, max, waits) | is the client side of PostgreSQL saturated |
| Runtime (USE) | `process_cpu_seconds_total`, `process_resident_memory_bytes`, `go_*` | CPU, memory, goroutines, GC |
| PostgreSQL | postgres-exporter: `pg_up`, `pg_stat_activity_*`, `pg_stat_database_*`, `pg_locks_count`, `pg_stat_statements_*` | server health, sessions, long transactions, locks, slow queries |
| RabbitMQ | built-in `rabbitmq_prometheus`: aggregated broker metrics and per-queue `rabbitmq_detailed_queue_messages_ready/unacked`, `_consumers` | queue depth and consumers |
| Pipeline | collector `otelcol_*` | are traces and logs being dropped |

**Traces** (Jaeger): one trace per API request; for `POST
/items/{id}/process` the trace continues through the outbox publish and the
worker's processing, with a span for each database query made under them.

**Logs** (Loki and container stdout): JSON records with `service`,
`version`, `trace_id`, `span_id`, and identifiers (`job_id`,
`work_item_id`, `delivery_id`, `route`). Loki indexes only `service_name` and
`deployment_environment_name`; everything else is structured metadata that
can be filtered (`| job_id="42"`) but adds no streams.

## Cardinality, cost, and retention

Measured in the lab at 2026-10-18 14:10 UTC, during experiment run 1, with
about 12 business requests per second (4 create → process → read iterations
per second):

| Signal | Volume | Driver |
| --- | --- | --- |
| Prometheus active series | 8,358 in total; API 161 (112 `work_items_*`), worker 94 (45) | infrastructure exporters dominate: RabbitMQ 3,300, Loki 2,307, PostgreSQL 1,250 (mostly `pg_stat_statements`), Prometheus 1,062 |
| Prometheus samples | about 826 per second | series × 1 / 10 s scrape interval |
| Prometheus disk | 3.4 MB after about 20 minutes | about 1–2 bytes per sample after compression |
| Loki ingestion | about 5.3 KB/s, of which about 5.1 KB/s is structured metadata | one record per request and per delivery; resource and identifier attributes repeated on every record |
| Loki streams | 2 | only `service_name` and `deployment_environment_name` are labels |
| Jaeger | about 20 spans per processed job, 2–3 per other request | one span per database query under a request or delivery |

Application series are bounded by construction: route patterns (7 business
and operational routes plus `unmatched`) × 2 methods + `other` × observed
status codes, plus fixed result/settlement/disposition labels. They do not
grow with users, items, or jobs. The risks that would make them grow, and
what prevents each:

- A concrete path (`/items/123`) as a label: the middleware labels the
  registered pattern, and unmatched paths share one value (tested).
- Job, item, delivery, or trace IDs as labels: identifiers go to logs and
  span attributes only (ADR 0012).
- Loki labels from attributes: the Loki OTLP configuration indexes only two
  resource attributes; the rest is structured metadata.
- Exporter defaults: `pg_stat_statements` adds a series set per distinct
  statement (bounded by the application's fixed queries, about 1,000 here);
  RabbitMQ per-object metrics are off, and per-queue depth comes from the
  `detailed` endpoint limited to two metric families.

Retention configured in the lab, and what it costs at the measured rate:

| Store | Retention | Estimated size at this load |
| --- | --- | --- |
| Prometheus | 15 days or 2 GB, whichever first | about 826 samples/s × 1.5 B × 86,400 s ≈ 107 MB/day → about 1.6 GB for 15 days |
| Loki | 7 days (compactor) | about 5.3 KB/s ≈ 460 MB/day uncompressed; chunks compress several times |
| Jaeger | in memory, at most 50,000 traces | lost on restart; at 4 jobs per second the newest few hours |

Cost choices and their trade-offs:

- Logs dominate volume. Per-request INFO records are kept because they are
  the operator's index from a symptom to a trace; at higher traffic the first
  reductions would be dropping repeated resource attributes from structured
  metadata (`telemetry_sdk_*`, `service_version` duplicated as `version`)
  and sampling successful request logs while keeping all warnings and errors.
- Traces are kept at 100 % (`parentbased_always_on`) because the lab traffic
  is small and failures are rare. Production would sample at the head (for
  example `OTEL_TRACES_SAMPLER=parentbased_traceidratio`) or keep error and
  slow traces with tail sampling in the collector; the trace context is
  always propagated, so logs keep their `trace_id` either way.
- SLO alerts depend only on Prometheus, the cheapest and most durable
  signal. Losing traces or logs degrades investigation, not detection.
- No cloud billing applies to this lab; the figures above are the inputs a
  hosted backend would charge for (series, samples, ingested bytes, spans).

## Limits of this lab

- Authentication is disabled and internal hops are plaintext; those controls
  are exercised in the security lab. Combining the two labs is not done here.
- Single host, single replica of everything. It shows behavior of one
  instance; it says nothing about multi-instance aggregation, high
  availability, or network partitions.
- Jaeger keeps traces in memory (lost on restart). Prometheus and Loki keep
  data on local volumes on the same disk as everything else.
- Alerts are evaluated by Prometheus and visible in its UI and API; no
  notifications are delivered (Alertmanager and on-call are #126).
- The load generator is a simple fixed-rate client for experiments, not the
  reproducible workload model that #123 needs.
