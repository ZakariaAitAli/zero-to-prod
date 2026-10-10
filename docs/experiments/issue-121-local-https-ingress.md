# Issue #121 — local HTTPS ingress and private backends (first slice)

Recorded 2026-10-10. This is the first implementation slice of Issue #121. It
does **not** complete #121: authentication, authorization, datastore TLS, rate
limiting, and secret rotation are later slices.

## Question

Can Work Items run behind one HTTPS entry point on host loopback, with private
backend services and runtime secrets that are not development defaults, and
can that exposure be measured rather than assumed?

Environment: **LOCAL-FIRST**. Network isolation, TLS verification, and secret
delivery are portable behaviors. Docker Desktop and WSL networking details are
specific to this workstation and are recorded as such.

## Exposure before the change

Recorded before any change, on `main` at `2b28df5` (the signed `sprint-03`
tag).

- The development lab containers `zero-to-prod-local-postgres-1` and
  `zero-to-prod-rabbitmq-rabbitmq-1` were exited, so nothing was listening.
  That is not evidence of isolation. Their configured publications are
  PostgreSQL `127.0.0.1:55432`, AMQP `127.0.0.1:5672`, and RabbitMQ management
  `127.0.0.1:15672`.
- No Windows or WSL listener existed on 5173, 5672, 8080, 8443, 9443, 15672,
  or 55432.
- The native development API binds `":" + PORT`. Because the development lab
  holds data that must be preserved, this was measured in a separate,
  temporary lab (`zero-to-prod-121-compat`, PostgreSQL 55436, AMQP 5675, API
  18080), which was destroyed afterwards:

  | From | Target | Result |
  | --- | --- | --- |
  | WSL | listener | `*:18080` (all interfaces) |
  | WSL | `127.0.0.1:18080` | reachable |
  | WSL | WSL `eth0` address | reachable |
  | Windows | `127.0.0.1:18080` | no connection |
  | Windows | WSL `eth0` address (WSL NAT network) | **reachable** |
  | Windows | Windows LAN and virtual adapter addresses | no connection |

  Reachability from another device on the LAN was not tested. The development
  API's behavior was not changed in this slice.
- Protected volumes, compared by name, creation time, and mount point before
  and after all work: `zero-to-prod-117_postgres_data`,
  `zero-to-prod-118-rabbitmq_rabbitmq_data`, `zero-to-prod-118_postgres_data`,
  `zero-to-prod-local_postgres_data`, `zero-to-prod-rabbitmq_rabbitmq_data`.
  All unchanged; their containers stayed exited. Contents were not inspected;
  no command targeted them.

## Ingress choice

Two conventional reverse proxies fit a single host-local entry point that
serves static files and proxies one upstream:

| | nginx | Caddy |
| --- | --- | --- |
| TLS with operator-supplied certificates | Yes | Yes |
| Automatic certificate management | No | Yes (ACME, internal CA) |
| Configuration model | Explicit directives | Concise Caddyfile with secure defaults |
| Static files, path allow-list, header replacement | Yes | Yes |
| Later needs (request limits, subrequest auth for a session gateway) | `limit_req`, `auth_request` built in | `rate_limit` is a plugin; `forward_auth` built in |
| Familiarity and documentation | Very widely deployed | Widely used, smaller footprint |

**Choice: nginx (provisional).** Caddy's main advantage, automatic certificate
management, would hide the certificate lifecycle that #121 must make explicit,
and the lab CA is deliberately operated by hand. nginx's explicit
configuration makes each boundary rule reviewable, and it has built-in request
limiting for the rate-limit slice. Revisit if automatic issuance becomes the
subject of an experiment.

## What was built

- `apps/work-items`: the API and worker accept `*_FILE` variants of their
  connection-URL settings (exactly one source; errors name the setting, never
  the value). A worker container image was added.
- `infra/security-lab/`: a separate Compose project
  (`zero-to-prod-security-lab`) with `edge`, internal `app`, and internal
  `data` networks. Only the ingress publishes a port, `127.0.0.1:9443`.
  API, worker, and ingress run read-only with all capabilities dropped and
  `no-new-privileges`. PostgreSQL identities come from secret files through
  the shared role script; RabbitMQ users, permissions, and the queue come from
  a generated definitions file. There is no default user and no management
  plugin.
- `tools/security-lab-local`: creates a name-constrained lab CA, an ingress
  certificate, and random runtime secrets outside the repository; checks them
  before start; starts, verifies, stops, and destroys only this lab.
- nginx: TLS 1.2/1.3 for `work-items.localhost` only (other names fail the
  handshake), the built Web UI, an allow-list of three API routes, replacement
  of client forwarding headers, a 64 KiB body limit, and an access log of path
  without query string.

See the [security lab guide](../guides/local-security-lab.md).

## Hypotheses and results

Evidence: [`local-https-ingress-verification.json`](../../evidence/issue-121/local-https-ingress-verification.json)
(53 results: 50 pass, 3 observations) and
[`missing-secret-failures.json`](../../evidence/issue-121/missing-secret-failures.json)
(8 pass). Both were produced by `tools/security-lab-local` on a freshly
destroyed and restarted lab, from the source in this change. The Windows LAN
address is redacted.

| Hypothesis | Observed |
| --- | --- |
| Only the ingress publishes a port, on 127.0.0.1 | Docker reports only `ingress=8443/tcp->127.0.0.1:9443`; Windows lists only `127.0.0.1:9443` among lab ports |
| A client verifying with the lab CA reaches the UI and API | WSL `curl` and Windows `curl.exe`: 200 with verification result 0 |
| Verification is actually enforced | Without the lab CA: curl exit 60 on WSL and Windows; another name: handshake rejected (exit 35) |
| API operational endpoints are not exposed | `/api/health` and `/api/version`: 404 at the ingress |
| The ingress reaches the API internally | 200 through the ingress; `wget http://api:8080/health` inside the ingress |
| Host clients cannot reach backends directly | No listener on 8080, 5432, 55432, 5672, 15672 on WSL or Windows loopback; no connection on Windows non-loopback addresses or WSL `eth0` |
| An unrelated container cannot reach backends | `api` unresolvable; API, PostgreSQL, and RabbitMQ container IPs time out from an unrelated network, the default bridge, and the ingress's own client network |
| Those failures are not caused by stopped services | Same probe from the `app` network resolves `api` and connects to `api:8080`; from the `data` network it connects to PostgreSQL and RabbitMQ |
| Missing secrets fail clearly | Preflight, API (missing file, directory artifact, both URLs), worker (both URLs), ingress (certificate), PostgreSQL (admin password): all exit 1 with a message naming the input; no secret value in output |
| Work Items semantics unchanged through the ingress | Create 201; process 202; job `succeeded`, `attempt_count` 1; item `done` with result (26 characters, 4 words); second process `409 work_item_already_done` |

### Secret exposure checks

Run against the final lab instance after the processing check: each of the six
generated passwords was searched for in all container logs (326 lines),
`docker inspect` output for every lab container, `docker image inspect` and
`docker history` for the three lab images, the Git tree, and this change's
documentation and evidence. No match. As a positive control, the same search found the app
password inside its own connection-URL file. Container environments contain
only `*_FILE` paths and no connection URLs.

### Development compatibility

In the temporary `zero-to-prod-121-compat` lab, using the development tools
and labelled defaults:

| Check | Result |
| --- | --- |
| `postgres-local start`, `migrate-up`; `rabbitmq-local start` | Migrations 1–5; broker healthy |
| `test-postgres-backup-validation.sh --integration` | Passed |
| API integration tests (CI environment, alternate ports) | 59 passed, 0 skipped |
| Worker integration tests (queue purged first, as in CI) | 102 passed, 0 skipped |
| `test-async-result-recovery.sh` | Passed |
| `work-items-api-local run` / `verify` with environment URLs | `/health`, `/ready`, `/version` passed |
| `gofmt`, `go vet` (including `crashexperiment`), `go test ./...` | Passed |
| CI policy tests, shellcheck, Compose config, actionlint | Passed |

## Unexpected behavior

1. **A missing Compose secret file does not fail.** Outside Swarm, Docker
   creates an empty directory at the mount point and the container starts.
   Compose `config` also passes. Fail-closed behavior therefore comes from the
   tool's preflight and from the applications rejecting a directory.
2. **Bind mounts keep the host owner and mode.** A mode-600 file owned by the
   WSL user was readable only by root or the same UID in containers. Mounted
   secret files are mode 644 inside a mode-700 directory instead.
3. **A loopback-only publication is still reachable from other containers.**
   An unrelated network, and the default bridge, connected to the ingress
   container's IP on 8443, and a verified HTTPS request succeeded that way.
   `host.docker.internal:9443` also connected. The container-loopback health
   port 8081 did not. Backends without published ports stayed unreachable.
   Any container on this Docker engine can therefore reach the ingress.
4. **Every host client appears as one address.** The ingress logged
   `172.22.0.1` (the edge network gateway) for all WSL and Windows requests.
   Per-address limiting would treat all host clients as a single client.
5. **Unknown names are slow to fail.** From non-lab networks, `api` took about
   4 seconds to return NXDOMAIN through the host resolver, longer than a 3-second
   TCP timeout. BusyBox `nslookup` also exited 0 after printing NXDOMAIN. The
   resolution check uses curl's resolver with a longer timeout.
6. **Host-to-container-IP probes are weak evidence here.** From WSL, lab
   container IPs returned "refused" or "timeout" depending on the run; the
   Docker subnets (for example 172.20.0.0/16) overlap a Windows virtual
   adapter (172.20.208.1). The containers live in the Docker Desktop VM,
   so these probes say little; the container vantage points with positive
   controls and the listener tables are the primary evidence.

## What this does not prove

- That the ingress is unreachable from another device on the LAN: no second
  device was used. Windows' own non-loopback addresses did not connect.
- Browser behavior beyond the
  [user-reported check](#user-reported-manual-verification): the automated
  checks above used `curl`, not a browser.
- Anything about authentication, authorization, CSRF, rate limiting, datastore
  TLS, certificate expiry behavior, or secret rotation.
- Isolation under a different Docker or WSL networking mode (for example WSL
  mirrored networking or Docker Engine without Docker Desktop).

## Manual verification required

1. Before trusting the lab CA, open `https://work-items.localhost:9443/` in
   the Windows browser: expect a certificate error.
2. Trust the CA ([guide](../guides/local-security-lab.md#trusting-the-lab-ca)),
   checking the SHA-1 thumbprint; reload: expect a valid connection issued by
   "Zero-to-Prod security lab CA".
3. List, create, and process a Work Item; expect `succeeded` and the result,
   with no console errors.
4. Remove the CA from the trust store when finished.
5. Optionally, from another device on the LAN, try the Windows LAN address on
   port 9443: expect no connection.

## User-reported manual verification

Reported by the repository owner on 2026-10-10, after the automated checks.
These are the owner's own observations in a Windows browser, not captured by
the lab tooling. No machine-readable evidence exists for them; the source of
each result is shown.

| Step | Result | Source |
| --- | --- | --- |
| Open `https://work-items.localhost:9443/` in Brave on Windows before trusting the lab CA | Browser showed `NET::ERR_CERT_AUTHORITY_INVALID` | Screenshot provided by the owner (not stored in the repository) |
| Trust the lab CA, then reload | Page loaded without a certificate warning | Owner's report |
| Create a Work Item titled "New Item" and process it | Item reached `done`; its job showed `succeeded`; result 8 characters, 2 words | Owner's report |
| `./tools/security-lab-local stop` | Lab stopped successfully | Owner's report |

The reported result matches the title-analysis rule for "New Item" (8 code
points, 2 words).

Not reported, so not verified: the certificate issuer shown by the browser,
the absence of console errors, removal of the CA from the trust store
(step 4), and the LAN check from another device (step 5).

## Remaining gaps

- No authentication or authorization; the lab must stay on loopback.
- Unencrypted internal hops (ingress → API, PostgreSQL, RabbitMQ).
- Ingress reachable from any container on the same Docker engine (finding 3).
- Host client addresses collapse to the gateway (finding 4).
- The native development API listens on all interfaces and is reachable from
  Windows through the WSL NAT address; unchanged here.
- No rotation, no revocation, single-tier CA, no CSP or HSTS, ingress image not
  built in CI, migrator URL in process arguments.
- #130 and #131 remain open and unmitigated; this slice does not touch restore,
  sequences, or message identity.

## Decision and capability levels

No ADR yet: the ingress choice is provisional, and the decisions #121 must
record (session model, authorization placement, transport policy) come in later
slices. No capability level changes; this slice is implementation without the
failure experiments the later slices add.

## Cleanup

The security lab is **kept** as scaffolding for the remaining #121 slices; its
exit decision is due when #121 closes. The temporary compatibility lab was
destroyed. The security lab was left running for the manual browser check;
the owner reported stopping it afterwards with `./tools/security-lab-local
stop`, which keeps its volumes. Remove it with `destroy` and its CA and secrets
with `purge-state --yes`.
