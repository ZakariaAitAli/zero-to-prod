# Local observability lab

Issue #122, [ADR 0012](../adr/0012-work-items-observability.md). An opt-in
Docker Compose lab that runs the Work Items API and worker with their
dependencies and a telemetry pipeline for local diagnosis and fault injection.

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
tools/observability-lab-experiments .local/reports/observability/new-run
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

## Cardinality, cost and retention

Keep metric labels bounded: registered route patterns rather than concrete paths,
and fixed method/result categories rather than user, job, delivery or trace IDs.
Identifiers belong in logs and span attributes. Review exporter defaults and Loki
index labels as well as application metrics.

Configured retention is 15 days or 2 GB for Prometheus, seven days for Loki and
an in-memory limit of 50,000 traces for Jaeger. Jaeger loses traces on restart;
all stores still share the host's failure domain. Measure ingestion and disk use
for the actual workload before choosing a storage or retention budget.

The lab samples all traces. Higher-traffic environments need a deliberate
sampling policy that retains useful failure and latency information. Reducing
logs or traces affects diagnosis; losing Prometheus or alert delivery affects
detection. Local resource use is not a cloud-cost estimate.

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
