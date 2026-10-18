# ADR 0012: Work Items telemetry, trace propagation, and SLOs

- Status: Proposed
- Issue: #122
- Date: 2026-10-18

## Context

Before this change the API and worker wrote unstructured `log.Printf` lines,
exposed no metrics, and had no tracing. An operator could not tell whether
users were succeeding, where time was spent, which dependency failed, or
whether any reliability objective was met. The asynchronous path (API →
outbox → RabbitMQ → worker) made this harder: a request that returns `202`
finishes long before its work does, and the outbox publisher may send the
message after a restart.

## Decision

### Signals and tools

| Signal | Produced by | Collected by | Stored in |
| --- | --- | --- | --- |
| Metrics | `prometheus/client_golang` on a private listener (`METRICS_ADDRESS`, `WORKER_METRICS_ADDRESS`; off when unset) | Prometheus scrape (pull) | Prometheus TSDB |
| Traces | OpenTelemetry Go SDK, OTLP/HTTP | OpenTelemetry Collector | Jaeger |
| Logs | `log/slog` JSON on stdout, and the same records over OTLP | Collector | Loki |

Metrics are pulled rather than pushed through the collector, so a collector
or trace-backend failure never hides the SLI data the alerts depend on.
Exporting telemetry is opt-in through the standard `OTEL_EXPORTER_OTLP_*`
variables; without them the services behave as before apart from JSON logs.

### Trace propagation across the asynchronous boundary

Only W3C `traceparent` (no baggage) is propagated.

1. The API starts a **new** server span for every request and ignores any
   inbound `traceparent`. Requests come from the internet through the
   ingress; an external caller must not choose trace IDs or force sampling.
2. Acceptance stores the request span's `traceparent` in the new nullable
   column `outbox_messages.trace_context` (migration 000007), in the same
   transaction as the job and outbox row. The column admits only the fixed
   traceparent format (CHECK constraint), and only the API role may read or
   write it.
3. The outbox publisher starts a producer span whose parent is the stored
   context, and sends `traceparent` as an AMQP header. Because the context is
   durable, a publish after an API restart or a broker outage still joins the
   original trace.
4. The worker starts one consumer span per delivery attempt, child of the
   header's context. Retries and redeliveries therefore appear as sibling
   spans in the same trace. Only the API's publisher identity may write to the
   queue, so the header comes from our own code.
5. Database queries made under a span are child client spans with the static
   SQL text and, on failure, only the SQLSTATE: PostgreSQL error messages can
   quote row values. Queries without a parent span (the outbox poll every
   500 ms) are not traced, so idle loops create no traces.

Considered and rejected:

- *Trace context in the message body.* It would change the versioned message
  contract (ADR 0003, delivery identity from ADR 0009) for metadata that has
  a standard transport slot.
- *Linking instead of parenting.* A span link is the semantic convention for
  batch consumers; here one request produces exactly one job, and a parent
  relationship gives a single trace an operator can open from the request.
  The cost is a trace whose duration includes queue time, which is what an
  operator investigating "why is my job late" needs.
- *otelpgx / otelhttp.* They trace every query including the idle outbox poll,
  and record more than needed; a 100-line local tracer and middleware keep
  the cardinality and data exposure explicit.

### Cardinality rules

Metric labels are bounded and enumerated in code: route **pattern**
(`/items/{id}/process`, never a concrete path), method (`GET`, `POST`,
`other`), status code, publish result, settlement, redelivered flag, and
completion/failure disposition. Job, work-item, delivery and trace IDs go to
logs and span attributes, never to labels. Loki stream labels are only
`service_name` and `deployment_environment_name`; identifiers are structured
metadata.

### Logs

Each record carries `service`, `version`, and, when logged with a request or
delivery context, `trace_id` and `span_id`. The API logs one record per
request (method, route pattern, status, duration; never query strings,
bodies, or headers) at INFO, or DEBUG for health/readiness/version probes.
The worker logs one record per delivery with job, work-item and delivery
IDs, the settlement, and the error when there is one. The outbox publisher
logs state changes (degraded/recovered), not every failed retry. Existing
message texts matched by the security-lab and experiment harnesses are kept;
the authentication logs keep their text and are correlated through the
request log of the same request.

### SLIs and SLOs

Defined and justified in [work-items-slos.md](../reference/work-items-slos.md):
availability 99.5 %, latency 99 % within 250 ms, and freshness 99 % of
accepted jobs completed within 30 s, over 28 days, with multi-window
burn-rate alerts and a symptom alert for stuck work.

## Consequences

- One additive migration (000007). Apply it before deploying the API that
  writes the column; an older API ignores it. A down migration exists.
- New dependencies: OpenTelemetry Go SDK and OTLP/HTTP exporters (traces and
  logs, v1.47.0 / log exporter v0.23.0), the `otelslog` bridge (v0.21.0), and
  `prometheus/client_golang` v1.25.0. The log bridge and exporter are
  pre-1.0 modules.
- The worker binary now has a `version` like the API (`-X main.version`).
- Without a collector, spans are still created (for log correlation) and
  dropped; the overhead is a few allocations per request.
- The backlog gauges are read by the API from PostgreSQL at scrape time (one
  small query per scrape); when the database is unreachable only
  `work_items_backlog_scrape_success 0` is reported.
- Not decided here: production sampling, multi-replica aggregation,
  notification routing and on-call (issue #126), and long-term storage.
