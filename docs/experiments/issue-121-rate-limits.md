# Issue #121 — ingress rate limiting

Date: 2026-10-18. [ADR 0010](../adr/0010-security-lab-rate-limiting.md).
Local Docker only; no production capacity or distributed-limiter claim.

The controlled-upstream test runs the production nginx configuration unchanged
with a private test CA and separate HTTPS upstream stubs. The stubs deliberately
do not implement authentication. All ingress-to-stub hops still verify their
service name and certificate. The script uses its own network, containers,
state directory and ephemeral loopback port, and removes them on exit.

[Admission evidence](../../evidence/issue-121/rate-limits/nginx-admission.json):

| Burst | Admitted (200) | Rejected (429) |
| --- | --- | --- |
| Bearer API, rotating spoofed forwarding headers | 48 | 72 |
| Session API | 46 | 74 |
| Identity-provider POST | 11 | 34 |
| Gateway sign-in initiation | 11 | 34 |

Every 429 had the documented JSON error and Retry-After header. A request after
refill succeeded. Authentication saturation left an API request available;
static resources and discovery remained reachable. Exactly 96 admitted API
requests reached the controlled API (including refill/isolation controls); no
rejected API request did. Logs recorded rejection without the placeholder
credential. Counts are timing-dependent; the test asserts admission and
rejection, bounded status types, response contract and forwarding counts, not
an exact scheduling-dependent threshold count.

The first fixtures needed correction: a single nginx process could re-route
mock requests by Host back through its public server, and the unprivileged mock
needed writable temporary paths. The final script uses separate TLS upstream
containers and the production configuration unchanged. These were fixture
problems, not evidence of successful application authentication.

A separate real security lab, `zero-to-prod-security-lab-limit`, then ran the
full authentication verification: **77 passed, 10 observations, 0 failed**,
including real sign-in, logout/replay, session refresh, expiry and dependency
outages. [Authentication evidence](../../evidence/issue-121/rate-limits/verify-authentication.json).
After the database secret-rotation experiment, a real headless Chromium UI run
also passed 17 checks; see the [rotation record](issue-121-secret-rotation.md).

The local application-TLS material/recovery script passed 77 checks and the
CI workflow policy script passed. CI now runs the Docker controlled-upstream
rate test. Aggregate limits across distinct client addresses, distributed
replicas, memory-zone exhaustion, sustained capacity and latency overhead were
not measured; they remain capacity/operations work. The rates are illustrative.
