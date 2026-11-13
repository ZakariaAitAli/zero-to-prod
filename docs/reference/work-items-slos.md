# Work Items SLIs, SLOs, and error budgets

Issue #122, [ADR 0012](../adr/0012-work-items-observability.md). Implemented
as Prometheus recording and alerting rules in
[`infra/observability-lab/prometheus/rules/slo.yml`](../../infra/observability-lab/prometheus/rules/slo.yml).

These are **initial** objectives for a single-instance reference system. They
are measured in the local observability lab only; no production traffic has
been observed, so the targets are a starting point to revise against real
measurements, not a commitment.

## Who the user is and what they need

A Work Items user (the web UI or an API client) does two things:

1. **Synchronous calls**: create and list items, request processing, read a
   job. They need an answer that is not a server error, quickly.
2. **Asynchronous processing**: after `202 Accepted`, they need the job to
   finish (title analysis stored, item `done`) within a reasonable time. A job
   that never finishes is the worst outcome and is invisible to request-level
   metrics.

## SLIs

All three are ratios of good events to valid events, measured server-side.

| SLI | Good event | Valid events | Source |
| --- | --- | --- | --- |
| Availability | response status < 500 | business requests: every route except `/health`, `/ready`, `/version`, and unmatched paths | `work_items_http_requests_total` |
| Latency | response within 0.25 s | business requests | `work_items_http_request_duration_seconds` bucket `le="0.25"` |
| Freshness | job completed within 30 s of acceptance | jobs completed in the window | `work_items_worker_job_completion_latency_seconds` bucket `le="30.0"` (database clock) |

Choices and their reasons:

- **4xx count as good.** A `404` for a missing item or a `409` for a job
  already active is the service working correctly. `503 persistence_unavailable`
  counts as bad: the user could not complete the action.
- **Operational probes are excluded.** Health and readiness probes would
  dilute the ratio with traffic no user sends.
- **Latency counts every business request, including failures.** A slow 503
  is still a slow experience; counting only successes would let timeouts
  disappear from the latency SLI.
- **Freshness is measured where the job finishes**, from `created_at` to
  `finished_at` in the same database, so clock skew between containers does
  not enter it. It cannot see jobs that never finish; the symptom alert on the
  oldest accepted job's age covers that gap (see below).

The worker now records `finished_at` with `clock_timestamp()` at the final
state update, so waits inside the completion transaction count toward latency.
A PostgreSQL lock-wait regression fails with the earlier transaction-start
`CURRENT_TIMESTAMP` and passes with the final-update clock. This still excludes
the small interval between that update and commit acknowledgement; it is a
near-completion database timestamp, not a separately observed commit clock.

Known measurement gaps:

- Server-side metrics cannot see requests that never reach the API (API down,
  ingress failure). The lab compensates with `WorkItemsAPIUnavailable`
  (scrape failure = outage) and records client-side results in experiments.
  A production deployment would add ingress or synthetic (black-box) probes.
- Single replica: there is no aggregation across instances to get wrong yet.
- The freshness SLI counts completions, so terminally **failed** jobs
  (attempts exhausted) are not counted as bad. With the current processor
  this cannot happen (it never fails); a processor that can fail needs a
  `failed` outcome in the SLI.

## SLOs

Window: rolling 28 days (four whole weeks, so the weekday/weekend mix is
stable).

| SLO | Target | Error budget over 28 days |
| --- | --- | --- |
| Availability | 99.5 % | 0.5 % of requests, about 3 h 22 min of total outage |
| Latency | 99 % within 0.25 s | 1 % of requests may be slower |
| Freshness | 99 % within 30 s | 1 % of completed jobs may take longer |

Why these numbers are credible for this workload:

- **Availability 99.5 %.** Every dependency is a single instance (one
  PostgreSQL, one RabbitMQ, one API, one worker) with no automatic failover
  (#124 is pending). A PostgreSQL restart, a migration with the API stopped
  (ADR 0009 requires one), or a host reboot each costs minutes. 99.9 %
  (about 40 min per 28 days) would be consumed by one unplanned restart plus
  one planned migration; 99.5 % leaves room for a few such events and still
  detects a real regression. The target should rise only after #124 provides
  failover.
- **Latency 0.25 s at 99 %.** The baseline experiment measured a client p99
  of a few milliseconds for create, process, and read at 12 requests per
  second ([experiment](../experiments/issue-122-observability.md)). 0.25 s is
  far above normal behavior, so crossing it means a real problem (lock waits,
  CPU starvation, pool exhaustion), and it is well below the 3 s database
  timeout, so it catches slowness before requests fail. It is also below the
  roughly 1 s at which an interactive user notices delay.
- **Freshness 30 s at 99 %.** Normal acceptance-to-completion is tens of
  milliseconds. The worker retries a failed session after 1 s and a processor
  failure up to three attempts, so a few retries still finish well within
  30 s; a broker or worker outage of more than about 30 s does not. 30 s is
  short enough that a user waiting on the page sees a stall as an incident,
  and long enough not to page for a single redelivery.

## Error-budget semantics

- The budget is the allowed bad fraction: 0.5 % (availability) or 1 % (latency
  and freshness) of valid events in the 28-day window. It is spent by bad
  events, not by time, so an outage during low traffic spends less budget
  than the same outage at peak. That matches user impact.
- **Burn rate** = observed bad ratio / allowed bad ratio. A burn rate of 1
  spends exactly the budget over 28 days; 14.4 sustained for one hour spends
  about 2.14 % of it; 6 sustained for six hours spends about 5.36 %.
- **Policy** (proposed; owned by #126): while more than half of the budget
  remains, feature work continues normally. When less than a quarter
  remains, changes that add risk (schema changes, dependency upgrades) wait
  unless they reduce risk, and the next work goes to the cause of the spend.
  Exhausting the budget triggers a postmortem. In the lab the observation
  window is far shorter than 28 days, so the lab shows burn rates, not a real
  remaining budget.
- Planned downtime spends budget too. It is the honest record of user impact
  and the reason the availability target is not 99.9 % yet.

## Alerts

Burn-rate alerts use two windows: the long window proves the burn is
significant, the short window makes the alert stop soon after recovery.

| Alert | Condition | Severity | Why it is actionable |
| --- | --- | --- | --- |
| `WorkItemsAvailabilityFastBurn` | bad ratio > 14.4 × 0.5 % over 1 h and 5 m, for 1 m | page | users are failing now; budget gone in about 2 days |
| `WorkItemsAvailabilitySlowBurn` | > 6 × 0.5 % over 6 h and 30 m, for 5 m | ticket | steady erosion; investigate in working hours |
| `WorkItemsLatencyFastBurn` | slow ratio > 14.4 × 1 % over 1 h and 5 m, for 1 m | page | users are waiting now |
| `WorkItemsLatencySlowBurn` | > 6 × 1 % over 6 h and 30 m, for 5 m | ticket | steady erosion |
| `WorkItemsFreshnessFastBurn` | late-completion ratio > 14.4 × 1 % over 1 h and 5 m, for 1 m | page | processed work is arriving late |
| `WorkItemsAsyncProcessingStalled` | oldest accepted job > 120 s, for 1 m | page | accepted work is not finishing at all; the freshness SLI cannot see it |
| `WorkItemsAPIUnavailable` | API scrape fails or is absent, for 1 m | page | the SLIs are blind and users are likely failing |
| `WorkItemsBacklogUnobservable` | backlog query fails, for 2 m | ticket | the stalled-work alert cannot fire |

Symptom-based, not cause-based: there is no page for "RabbitMQ down",
"PostgreSQL down", or "worker down". Those causes page through the symptom
they produce (stalled work, failing requests), and the dashboard and runbook
identify the cause. A RabbitMQ outage that users do not feel (the outbox
absorbs it for less than two minutes) should not wake anyone. Cause metrics
(target `up`, broker connection failures, pool waits) are on the dashboard
for diagnosis.

Not done here: notification delivery (Alertmanager routing, on-call
schedules) belongs to #126; in the lab, alerts are observed in Prometheus.
