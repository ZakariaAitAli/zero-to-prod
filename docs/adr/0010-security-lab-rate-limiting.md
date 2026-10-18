# ADR 0010 — Security lab ingress admission control

## Status

Proposed. Implements the rate-limit experiment in #121. Local Docker scope;
these illustrative budgets are not measured production capacity or SLOs.

## Drivers and alternatives

The ingress should reject bursts before forwarding expensive requests, bound
aggregate admission, preserve explicit authentication/authorization decisions
for admitted requests, and avoid trusting caller-supplied forwarding headers.

| Option | Tradeoff |
| --- | --- |
| Fixed-window counter | Simple; permits a double burst across window boundaries |
| Ingress leaky bucket | Existing nginx supports shared counters and immediate excess rejection; local per-instance state |
| Application/principal quota | Fairer by authenticated identity but verification and forwarding already consume resources; requires a separate decision |
| Distributed limiter | Coordinates replicas but adds a shared dependency, failure policy, latency, and state lifecycle |
| Delay excess requests | Smooths bursts but retains requests and connections during overload |

Use nginx's leaky-bucket limiter, bounded bursts, and `nodelay` shedding.
Compare measured overload and capacity under #123 before choosing deployment
budgets or adding distributed quotas. Do not treat a limiter as DDoS protection.

## Policy

| Surface | Per connecting IP | Aggregate per ingress | Burst allowances |
| --- | --- | --- | --- |
| Allow-listed API requests, session and bearer | 20/s | 100/s | 40 per IP; 200 aggregate |
| Gateway sign-in/callback/sign-out and POSTs to the exposed realm | 5/s | 20/s | 10 per IP; 40 aggregate |

Keys use the original host/method/path and TCP peer address. `X-Forwarded-For`,
`X-Real-IP`, and `Forwarded` cannot select the budget. The original URI remains
available through the bearer internal redirect, so neither API path skips the
limiter. The main request is accounted once, before proxying/authentication
subrequests; controlled-upstream tests verify rejection prevents forwarding.
Static resources, discovery GETs, health, and loopback diagnostics have empty
keys and consume no quota. Invalid routes and early CSRF/credential refusals
may be returned before admission; they do not forward to a backend.

Excess requests return 429 with `{"error":"rate_limited"}` and `Retry-After: 1`.
This response preserves security headers and a request ID. The access log adds
`limit=PASSED|REJECTED` without recording tokens, cookies, query strings, or
passwords. Error logs on public servers retain their existing credential-safe
suppression. Authentication saturation uses a different budget from the API.

## Consequences and limits

All clients behind one NAT or Docker forwarding address share the IP budget;
this is not user fairness or account lockout. A client cannot evade it by
inventing a forwarding header, but distributed clients can consume aggregate
capacity. Quotas reset on ingress restart and are not coordinated across
replicas. They do not bound total open sockets, TLS handshake cost, sustained
processing backlog, or datastore usage below the admission threshold.

Do not automatically retry POSTs after ambiguous upstream errors. A 429
rejected before forwarding can be retried with backoff, respecting Retry-After;
a timed-out admitted request requires the existing business/API semantics.

The [experiment](../experiments/issue-121-rate-limits.md) distinguishes
controlled-upstream admission tests from real authentication regressions.
Source: [nginx limit_req module](https://nginx.org/en/docs/http/ngx_http_limit_req_module.html).
