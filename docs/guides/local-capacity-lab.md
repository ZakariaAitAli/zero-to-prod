# Local capacity and scaling lab

Issue #123. This is a single-host Docker experiment, with authentication off
and synthetic titles only. It extends the observability lab from #122; it
is not a production deployment profile. See the [experiment](../experiments/issue-123-capacity.md)
for measured results and their limits.

## Before running

Docker must support Compose `!reset` and the pinned images. Allow free host
loopback ports 18081, 18082, 19091, 13001 and 16687, and subnet 10.213.21.0/24.
The driver rejects overlapping Docker subnets before starting containers.
Check host/VPN routes separately if the host networking changes.

```sh
bash tools/capacity-lab-local up
python3 tools/capacity-lab-experiments evidence/issue-123/new-run
bash tools/capacity-lab-local stop
```

The output directory must not exist. Optional stage names after the output
path run a subset; see `STAGES` in the harness. The harness stops only its own
project in a `finally` block. No volume, image, network or state is deleted.
The driver keeps secrets outside Git in
`~/.local/state/zero-to-prod/capacity-lab`, a private directory. Do not print
or commit those files. Do not run concurrently with another experiment in
this project: pool settings and replica counts are deliberately changed.

The project `zero-to-prod-capacity-lab` has its own PostgreSQL, RabbitMQ,
telemetry volumes, network and images. Previously retained labs are not used.
The database accumulates test rows across stages; every stage records its own
unique title prefix and checks global business invariants. This avoids
resetting storage, but means later stages run on a larger database and warmer
caches. Results are one ordered run, not randomized independent trials.

## Workload and measurements

An iteration represents one user submitting a short Work Item: create,
request processing, then read the accepted job once. This is three requests
when all operations succeed. It intentionally does not poll until completion;
completion latency and eventual outcomes come from database timestamps.
Long titles, expensive processing, UI assets, authentication, retries from a
real client and multi-tenant contention are outside this workload.

A fixed schedule starts iterations using at most 128 concurrent client
threads. An arrival with no free slot is counted as a client drop, rather
than queued indefinitely. Every HTTP response, timeout and transport failure
is counted; client failures use status 0. Requests have a 10-second timeout.
Latency is measured around each attempted HTTP request, not from the
scheduled arrival time, so scheduling lag/client drops must be read alongside
latency: this is not an unlimited-load or coordinated-omission-free benchmark.
The client, Docker and dependencies share a host.

The harness reports request p50/p95/p99, status counts, achieved throughput,
request availability and the 250 ms fraction. Database queries report job
counts, completion p50/p95/p99, the 30-second fraction and invariant violations.
A scrape snapshot reports pool waits, connections, queue/outbox backlog,
process CPU/memory and discovered replica counts. Docker resource snapshots
cover the whole dedicated stack. Short stages are useful for contrast,
not for a 28-day SLO compliance claim.

API and worker replicas use the same immutable local image identity during a
run. The gateway resolves all API addresses dynamically and balances them;
POSTs are not retried by the gateway. Prometheus DNS discovery scrapes every
replica separately. API pool limits are set through its protected database
URL, followed by API recreation. Each scaled API/worker gets the stated
per-instance CPU allowance; scaling increases total allocated CPU. This
compares adding capacity, not a claim that two instances outperform one with
the same total CPU.

Backlog stages stop workers while accepting work, then measure draining with
one or three workers at the same per-worker CPU cap. This isolates consumer
capacity from admission. Global invariants and a concurrent repeated-submit
probe check that multiple API/worker processes do not create extra jobs,
results or outbox rows for one item.

## Admission comparison

Port 18081 has admission disabled. Port 18082 uses a per-connection-IP leaky
bucket at 20 requests/s with burst 40, immediate rejection, JSON 429 and
Retry-After 1. These match the API per-IP parameters in ADR 0010, but this
HTTP gateway is a controlled comparison, not the security-lab gateway: it
omits TLS, authentication, aggregate/authentication zones and security routing.
Every load-generator connection shares one IP. A 429 counts as good in the
#122 server-error availability SLI but is rejected user work; inspect status
counts and accepted jobs, not availability alone. The client does not retry
429s, which makes shed work explicit.

## Cost boundary

Docker CPU percentages are sampled, not billed CPU-seconds; memory is sampled
resident/container usage rather than a reservation. The experiment reports
approximate resource units per successful job and an explicit model for
projecting resource costs. There is no cloud bill, measured electricity,
network egress charge or independent-host price in this lab. Telemetry is
included in stack measurements and can dominate a small application.
Use actual target-environment prices and redundancy/retention requirements
before estimating a deployment budget. Ten minutes of sustained load cannot
prove day/week/month stability, memory-leak absence or production capacity.
