# Issue #123 — local capacity, scaling and overload

Date: 2026-10-18. Scope: the user-selected local Docker lab, one WSL/Docker
Desktop host. Method: [local capacity lab](../guides/local-capacity-lab.md),
[ADR 0013](../adr/0013-local-capacity-measurement.md). Initial objectives:
[Work Items SLOs](../reference/work-items-slos.md).

## Reproduction and provenance

The harness `tools/capacity-lab-experiments` defines the workload, matrix and
resource limits. Run 2 is recorded under `evidence/issue-123/run2/`. An earlier
interrupted trial is retained in run1 with its instrumentation corrections:
old Prometheus target samples survived container recreation, and shared
backlog gauges must be aggregated with max rather than sum. Run 2 filters
application measurements to actual current service endpoints and waits for
scrape discovery before load.

The built application derives from PR #142. Runtime image identities, source
hashes, commit/tree identities and final state are recorded with the evidence.
The application was not optimized during this measurement. The driver,
configuration and load harness bytes are fixed for run 2. Documentation and
the reporting helper do not run in the application images.

Every stage uses synthetic short titles and a create → accept → read-job
iteration. At most 128 client threads issue scheduled arrivals; drops are
explicit, not server errors. API/worker scaling adds resources, with the
same CPU cap per replica. PostgreSQL has 1 CPU and RabbitMQ 0.5 CPU. Except
for the ten-minute sustained stage, load phases last 30 or 60 seconds, plus
startup, outstanding requests and backlog drain. These are ordered single
runs on an accumulating database, not randomized/repeated benchmarks.

## Results (run 2)

| Stage | HTTP req/s | Request p99 ms | No 5xx/transport error | Within 250 ms | Jobs accepted | Within 30 s | Client drops |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | 60.0 | 5.2 | 100.00% | 100.00% | 1200 | 100.00% | 0 |
| ramp-60 | 180.0 | 8.0 | 100.00% | 100.00% | 3600 | 100.00% | 0 |
| ramp-120 | 360.0 | 66.8 | 100.00% | 100.00% | 7200 | 100.00% | 0 |
| saturated-api | 44.1 | 4097.5 | 67.44% | 0.21% | 493 | 20.49% | 2388 |
| two-apis | 114.6 | 2612.4 | 100.00% | 7.06% | 2390 | 1.05% | 1210 |
| pool-one | 180.0 | 124.4 | 100.00% | 99.86% | 3600 | 100.00% | 0 |
| pool-sixteen | 180.0 | 78.0 | 100.00% | 99.88% | 3600 | 100.00% | 0 |
| one-worker-backlog | 120.0 | 5.2 | 100.00% | 100.00% | 1200 | 0.00% | 0 |
| three-workers-backlog | 120.0 | 7.5 | 100.00% | 100.00% | 1200 | 66.83% | 0 |
| overload-unlimited | 39.2 | 4197.8 | 50.29% | 0.20% | 229 | 86.03% | 3394 |
| overload-limited | 100.6 | 625.9 | 100.00% | 98.61% | 2 | 100.00% | 0 |
| sustained | 60.0 | 15.4 | 100.00% | 99.92% | 12000 | 65.48% | 0 |

Percentages describe attempted requests and completed accepted jobs in each
stage, not a rolling 28-day budget. Dropped client arrivals are excluded from
HTTP latency and are shown separately. Accepted jobs are counted in the
database; they can exceed observed 202 responses when overload causes an
ambiguous commit/timeout. Every accepted job in these stages eventually
succeeded; no result/job/outbox invariant violation was found. Multi-API
stages additionally passed concurrent repeated-submit probes with exactly
one job, one result and one outbox row for the item.

## First meaningful bottleneck

At 120 jobs/s (360 HTTP requests/s), the API's end-of-stage CPU rate was
0.244 cores against its 0.25-core cap. The outbox held 1,103 unpublished jobs,
while the broker ready queue was empty. Pool wait was about 0.0012 seconds
per second; PostgreSQL's sampled CPU mean was about 0.33 cores against its
1-core allowance, and the worker had headroom. This points to the capped API
process, which also runs the serial outbox publisher, rather than a busy
broker or worker. This is evidence for this resource/workload combination,
not proof of the database's ultimate capacity.

Request p99 was still 66.8 ms, but job p99 rose to 15.9 s and draining took
15.7 s after arrivals stopped. The finite stage met the initial latency and
freshness thresholds, yet backlog growth means **120 jobs/s is not an
established sustainable rate**. At 60 jobs/s the same single-API profile had
8 ms request p99 and 1.3 s completion p99 without client drops.

With one API capped at 0.02 CPU, 60 scheduled jobs/s caused 916 server errors,
about 4.1 s request p99, and 2,388 dropped client arrivals. The pool wait rate
reached about 78 accumulated seconds per second: concurrent requests were
waiting, not a claim that one second of wall time lasts 78 seconds. All
493 persisted accepted jobs completed, but only 101 did so within 30 seconds.
A few accepted commits had no observed 202: callers must reconcile an
ambiguous timeout instead of assuming no state change.

## Scaling and connection pools

Two APIs at 0.02 CPU each accepted 2,390 jobs without observed 5xx errors,
versus 493 with one. However, 1,210 arrivals were still dropped at the client
cap, request p99 was 2.6 s, and job p99 was 65 s. Adding API instances helped
synchronous throughput but did not establish end-to-end SLO capacity.
The outbox publisher still competes for those API CPU budgets.

At 60 jobs/s, two APIs at 0.1 CPU each with a one-connection pool had request
p99 124 ms, completion p99 2.56 s and about 0.80 seconds/s of aggregate pool
wait. Raising each API's pool maximum to 16 reduced request p99 to 78 ms and
completion p99 to 1.28 s. Both profiles completed 3,600 jobs without errors.
This supports a measured improvement within the tested range; it does not
justify unbounded pools. Each replica consumes a separate pool and the
database connection budget must include all APIs, workers and monitoring.

Holding all workers stopped accumulated 1,200 jobs. One worker at 0.03 CPU
drained them in 67.6 s, about 17.7 jobs/s. Three at 0.03 CPU each drained them
in 21.3 s, about 56.3 jobs/s. This is roughly 3.2x faster with 3x worker CPU,
within noise for these single runs, not superlinear scaling established by
repeated trials. Invariants and duplicate-submit probes passed with the
multiple worker/API processes.

The relationship L = λW is useful only for a stationary queue. Backlog grows
in the overload stages, so applying a steady-state Little's Law estimate to
them would be misleading. The backlog experiments instead report initial
work, observed drain time and eventual outcomes. They do not measure an
integrated time-average backlog for a precise Little's Law validation.

## Admission, backpressure and overload failure

Both overload stages offer 80 iterations/s to one 0.02-CPU API. Admission off
produced 1,272 503s, about 4.2 s request p99, 3,394 client drops and 229 accepted
jobs. Admission on used the API per-IP parameters from ADR 0010 (20 requests/s,
burst 40), but in the controlled plaintext HTTP gateway described in the guide.
It returned 4,799 429s, no 5xx, no client arrival drops, and about 626 ms p99.
Only **two jobs** were accepted: new create calls took the shared IP budget
before follow-up processing calls arrived. The generator deliberately does
not honor Retry-After or retry the shed step.

This is an observed fairness/workflow limitation, not a successful user-flow
capacity result. The limited run still missed the 99%-within-250-ms target
(98.61%). Its availability SLI showed 100% because 429s are not 5xx. A useful
production policy needs separate rejection/workflow-success monitoring,
cooperative backoff, suitable per-operation/principal fairness and an
aggregate resource budget. Merely counting fast rejections as good responses
would hide the user impact. NAT-sharing clients can face the same competition.

The outbox protects accepted work during an outage/slow publisher, but is not
a bounded admission queue: storage can continue growing. The worker bounds
in-flight delivery and tolerates duplicates, but infrastructure failures
still requeue at a fixed interval and can block following work (observed in
#122). This experiment adds evidence and does not change that retry policy.
There is no tested production overload guarantee for arbitrary workloads.

## Sustained traffic and cost

The final run-2 stage is ten minutes at 20 iterations/s with one API and one
worker, 0.25 CPU each, and the broker capped at 0.5 CPU. All 36,000 HTTP
requests succeeded, request p99 was 15.4 ms and 99.92% were within 250 ms.
All 12,000 accepted jobs eventually succeeded, but only 7,858 (65.48%) met
30 seconds; job p99 reached 246 s. This is a **freshness failure**, despite
healthy request metrics.

The later bottleneck moved to the broker: database query means stayed below
1 ms with no pool wait, application CPU had headroom, but publisher confirms
averaged about 169 ms and the serial publisher managed only 5–6 jobs/s.
RabbitMQ was consuming its 0.5-CPU allowance. After the 600-second load
stopped, a recorded diagnostic changed only that owned container's cap to
2 CPUs; observed drain throughput rose above 60 jobs/s. This intervention
occurred during drain, not during the offered-load phase, and is recorded in
`sustained-drain-intervention.json`. The final 196-second drain and resource
estimate therefore include that intervention and are **not** a fixed-budget
sustained success. No database optimization or extra worker was needed for
this diagnostic; it supports broker CPU/confirmation delay as this later
constraint rather than a database bottleneck.

Run 2's full-stack sample mean was approximately 0.77 CPU cores and 1.47 GiB,
with about 51.1 sampled CPU-seconds and 0.0271 GiB-hours per thousand completed
jobs including load and recovery. These mixed-budget recovery units describe
this actual failed run; they are not a steady-state deployment cost.

The clean ten-minute confirmation with a 2-CPU broker is recorded separately
as [run 3](../../evidence/issue-123/run3/sustained.json). All 36,000 requests
succeeded, all were within 250 ms, request p99 was 35.6 ms, and all 12,000 jobs
completed within 30 seconds (completion p99 0.521 s). There were no client
drops or invariant violations; the final drain took 0.68 s. This establishes
this **20 jobs/s, ten-minute local profile** as an observed success, not a
longer-term or production capacity guarantee. No post-load CPU intervention
was made in run 3.

Run 3 also includes the independently tested final-update completion
timestamp correction from PR #142; run-2 source/images remain pinned and
unchanged. Configuration/harness hashes match their recorded sources. The
processes restarted between runs, caches/database size changed, and these
are single runs: do not attribute every resource difference solely to CPU.

Run-3 whole-stack sample means were 1.265 CPU cores and 0.925 GiB. The
[regenerable report](../../evidence/issue-123/run3/analysis.json) estimates
63.3 CPU-seconds (0.0176 vCPU-hours) and 0.0129 GiB-hours per thousand successful
jobs, including the tiny final drain. These are measured resource units for
this finite synthetic workload. A larger CPU allowance made reliable
completion possible but did not make resource consumption zero or prove
optimal cost. Telemetry and broker housekeeping are included in the stack;
client/host/build/operations overhead is not. The sampled application API
and worker CPU means were about 0.146 and 0.084 cores, respectively, and
PostgreSQL about 0.221 cores; the rest includes broker and telemetry work.

The cost calculation uses Docker CPU/memory snapshots for the **whole
isolated stack**, including telemetry. CPU-seconds and GiB-hours per thousand
successful jobs are approximate sampled resource units, not billed usage.
The reporting helper includes load and drain duration; sampling and scrape
queries add small boundary error. It excludes host OS, client CPU, Docker VM
overhead, build/startup, operator time, electricity and hardware depreciation.
A cost projection is:

`resource cost / 1000 jobs = (CPU-seconds / 3600) × price per vCPU-hour + GiB-hours × price per GiB-hour + storage/egress/backup/operations allocation`.

No provider prices or cloud spending were introduced. Actual target deployment
prices, reservations, replica/failure-domain needs and data retention are
required before a monetary budget. The current application retains its data
without a tested archival/deletion policy; a finite traffic run cannot prove
a steady-state storage cost. Trace/log retention is bounded as documented in
the observability guide, but trace storage is in-memory and not durable.

## Limits and next actions

- Authentication, TLS and realistic/expensive processors are absent from this
  workload. The security lab proves different controls, not this capacity.
- The load generator shares the host; its dropped arrivals explicitly limit
  the saturated measurements. HTTP latency excludes scheduling lag.
- Completion timestamps in the built worker use transaction-start time and
  can undercount time inside the completion transaction. These uncontended
  processing workloads use that lower-bound proxy; do not treat it as exact
  commit-visible latency. The separate PostgreSQL lock-wait regression now proves the timestamp
  correction in PR #142; run 3 uses that corrected binary. Neither the old
  nor new update timestamp includes the subsequent commit acknowledgement.
- One host is a common failure domain. No independent-node/region result,
  production budget, long-term memory-leak result or 28-day SLO compliance is
  established. Ten minutes is a local sustained check, not an operational soak.
- All results are single ordered runs. Database growth, caches and telemetry
  retention differ over time. Repeat/randomize and use an independent client
  before making deployment-sizing decisions.
- #124 owns failover and recovery; #125 owns safe live delivery; #126 owns
  operational response, longer sustained traffic and the readiness verdict.
  The rejection/workflow SLI gap and timestamp measurement limit must be part
  of that verdict. No issue should close merely because this lab exists.
