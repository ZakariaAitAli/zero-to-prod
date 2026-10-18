# ADR 0013: measure local capacity with a bounded arrival schedule

- Status: accepted for the local learning lab
- Date: 2026-10-18
- Issue: #123

## Context

The system has durable acceptance, idempotent processing and initial
observability, but no measured capacity envelope. A statement that it scales
needs a workload, latency/error objectives, resource limits, and invariant
checks. The selected environment is a single Docker/WSL host. Results must
not imply independent-host resilience or cloud cost.

## Alternatives

A closed-loop client with fixed concurrency is simple and bounds resources,
but slows its offered rate when the service slows, potentially concealing
queue growth. An unlimited open-arrival generator keeps arrivals independent
of service speed but can exhaust the client/host before the system. A remote
load service isolates the generator, but adds infrastructure and cost beyond
the selected local scope. A standardized load engine provides richer
scheduling/HTTP statistics but does not replace workload design or business
invariant checks.

## Decision

Use a stdlib client with scheduled fixed-rate iterations and a hard 128-thread
cap, count dropped client arrivals explicitly, and keep request timeout/error
counts. Measure latency of attempted requests; explicitly disclose that it
excludes scheduling delay. Use the same create/accept/read workload across a
controlled matrix, then a ten-minute sustained stage. Preserve trial failures
and separate harness corrections from system changes.

Use a separate Docker project, network, secrets and volumes, pinned external
images, and fixed per-instance CPU budgets. Vary API replicas, worker replicas
and API pool maxima deliberately. Scaling adds total resources; it does not
establish equal-budget efficiency. Scrape actual current replica addresses
and take the maximum of shared database gauges across APIs. Do not add
replicas and sum duplicate database state.

Keep control and admission-limited HTTP entry points distinct. Compare
accepted work, rejected requests, latency and eventual completion, since 429
responses are not 5xx but still represent user work shed. This is a local
admission comparison using the API per-IP parameters from ADR 0010, not a
replacement security gateway.

Verify eventual database/job outcomes after each stage and concurrent
repeated submissions with multiple APIs. Stop on violated invariants or a
backlog that will not drain; retain data for investigation. Preserve all
previous labs and never prune Docker as experiment cleanup.

## Consequences

The result is a reproducible envelope for one small synthetic workload, with
CPU/memory and queue/pool evidence, not a production maximum. A single ordered
run has warm-cache/database-growth effects. The client shares the host and
can become the bottleneck. Telemetry itself costs resources; include it in
stack estimates and separately report application/dependency CPU. Sampled
resource units can inform a cost model, but are not a provider bill or a
power measurement. Longer traffic, realistic processing, authentication and
TLS, independent load hosts, and repeated/randomized trials remain necessary
before production sizing.
