# ADR 0006 — Security lab internal transport: where TLS applies inside the lab

## Status

Proposed (provisional). Builds on ADR 0004 and ADR 0005, which are also
Proposed and unchanged by this decision.

## Context

The security lab terminates TLS at the ingress. Behind it every hop was
plaintext on Docker internal networks: the ingress to the API, gateway, and
identity provider; the gateway to its session store; Keycloak to its
database; and the API, worker, and migrator to PostgreSQL
(`sslmode=disable`) and RabbitMQ. Issue #121 asks which traffic stays
encrypted internally, who owns certificates and renewal, and what happens
when a certificate is invalid.

The [threat model](../architecture/security-lab-threat-model.md) maps each
connection, the members of its network, and what crosses it. Two facts drive
this decision:

- **Network membership differs.** `data` has up to five members: the API and
  worker (all capabilities dropped), PostgreSQL and RabbitMQ (official images
  with Docker's default capabilities, including `CAP_NET_RAW`, and without
  `no-new-privileges`), and the migrator on demand (image default user, no
  dropped capabilities). `app` has three members, all with every capability
  dropped, `no-new-privileges`, and read-only file systems. `idp`,
  `idp-data`, and `session` each have exactly two members, the two ends of the
  connection.
- **What crosses differs.** AMQP `PLAIN` sends RabbitMQ passwords themselves;
  PostgreSQL uses SCRAM, so its passwords do not cross, but all business data
  does. The ingress hops carry tokens, session tickets, and (to Keycloak) person
  passwords.

## Decision drivers

| ID | Criterion |
| --- | --- |
| R1 | Encrypt where a third party on the same network could plausibly observe or impersonate (threats T1–T3) |
| R2 | Verification that cannot be skipped: server identity and name, no plaintext fallback (T6) |
| R3 | Failures must be closed and observable at the boundary, and recoverable without rebuilding the lab (T5) |
| R4 | Issuance, trust distribution, renewal, and ownership reproducible by the lab tool |
| R5 | A browser-trusted CA must not be able to vouch for internal services, and the internal CA must vouch for nothing but the exact internal service names (T7) |
| R6 | No application code change unless the drivers cannot express the policy |
| R7 | Lifecycle cost proportionate to the threat removed |

## Options

| Option | Addresses | Leaves open | Cost |
| --- | --- | --- | --- |
| **A. Plaintext on private networks** (status quo) | External reachability only (measured in #133) | T1–T4, T6 on every hop | None |
| **B. Server-authenticated TLS** (client verifies server certificate and name; client authenticates with its existing password inside TLS) | T1, T2, T3, T4, and T6 on the hops where it is applied | Client identity remains a password readable in the client's container; CA key protection; renewal | One CA, one certificate per server, CA file per client; reload procedure per server |
| **C. Mutual TLS** (B plus client certificates; optionally certificate-based authentication replacing passwords) | B, plus: a stolen password alone no longer suffices | In this lab the client key would be a file mounted next to the password file, so the theft path that yields one yields the other | A key and certificate per client (API, worker, migrator; per broker user), distribution into read-only containers, rotation, and server-side mapping of certificates to roles and users |

A service mesh (sidecar-issued mTLS everywhere) was not considered credible
for a Compose lab on one engine.

mTLS is not assumed to be required everywhere. Its additional benefit over B
is client authentication by something other than a shared secret. That pays
off when client keys are harder to steal than passwords (hardware-backed or
attested workload identity) or when passwords are long-lived and shared. In
this lab they would sit in the same mounted directory as the passwords, so the
gain is small and the lifecycle cost roughly doubles.

## Provisional decision

**Option B on the `data` network; A elsewhere, explicitly and with triggers.**

| Hop | Decision | Reason |
| --- | --- | --- |
| API, worker, migrator → PostgreSQL | TLS, `sslmode=verify-full` with the internal CA | `data` has members with raw-socket capability; business data |
| API, worker → RabbitMQ | TLS (`amqps`, port 5671), server and name verified | As above, and `PLAIN` passwords cross this hop |
| Ingress → API, ingress → oauth2-proxy | Plaintext (provisional) | All `app` members drop every capability and run with `no-new-privileges`, so capturing requires a kernel escape. Next candidate once any member loses those restrictions |
| Ingress → Keycloak | Plaintext (provisional) | Two-member network: only the peer can observe. Highest-value plaintext hop (person passwords); first to encrypt if `idp` gains a member or the lab spans hosts |
| oauth2-proxy → Valkey, Keycloak → its PostgreSQL | Plaintext (provisional) | Two-member networks |

Details:

- **Separate internal CA, limited to the exact service names.** ECDSA
  P-256, 90 days, `pathlen:0`. Its name constraints are:
  - permitted `DNS:postgres` and `DNS:rabbitmq`;
  - excluded `DNS:.postgres` and `DNS:.rabbitmq`;
  - all IP addresses excluded.

  A permitted dNSName constraint alone permits the whole DNS subtree
  (RFC 5280): `extra.postgres` would also be accepted. That was the first
  version of this slice; review found it, and the claim that the CA could
  vouch for nothing else overstated the guarantee. The exclusions remove the
  subdomains and keep the names themselves, because OpenSSL (libpq) and Go's
  `crypto/x509` (pgx, amqp091-go) both treat a leading dot as "proper
  subdomains only".

  RFC 5280 does not define the leading dot for dNSName, so a verifier outside
  these two may read it differently. The behavior is tested with OpenSSL in CI
  and with libpq and the Go clients in the lab, using a certificate that names
  both `postgres` and `extra.postgres`.

  The lab CA, which people may trust in Windows or a browser, does not verify
  the internal servers.
- **CA names versus the dialed name.** The CA's constraints bound which names
  any certificate it issues may carry: exactly `postgres` and `rabbitmq`.
  Separately, each client verifies the one name it dialed. A valid
  `rabbitmq` certificate presented by the PostgreSQL address is refused by
  that check, not by the constraints.
- **Server certificates.** One per service, 14 days, SAN equal to the Docker
  service name, `serverAuth`. Key and certificate in one `tls.pem`, replaced
  by a single rename in a per-service directory (mode 711, file 644), mounted
  read-only, as for the ingress. PostgreSQL refuses a key it does not own, so
  its entrypoint copies the file into a tmpfs owned by `postgres` with mode
  600 (`infra/security-lab/postgres/tls.sh`).
- **Server-side enforcement.** PostgreSQL uses a repository `pg_hba.conf`:
  `hostssl … scram-sha-256` and `hostnossl … reject` (the local socket stays
  `trust`, as in the image). RabbitMQ has `listeners.tcp = none` and offers
  TLS 1.2/1.3 only. A misconfigured client therefore fails instead of falling
  back.
- **Client-side verification without code changes.** The generated URLs use
  `sslmode=verify-full&sslrootcert=/internal-ca/ca.crt` (pgx in the API and
  worker; the same URL for the migrator's driver) and `amqps://…@rabbitmq:5671/…?cacertfile=/internal-ca/ca.crt`
  (amqp091-go v1.15.0, which takes the server name from the host and never
  disables verification on this path; read in its source). `check-secrets`
  refuses any other form, including `sslmode=require`, which encrypts without
  verifying.
- **Trust distribution.** The internal CA certificate (public) is mounted
  read-only into the API, worker, and migrator only. The ingress, gateway,
  Keycloak, and browsers never receive it.
- **Renewal and reload.** `issue-internal-certificate <service>` issues,
  replaces the file, reloads (PostgreSQL `pg_ctl reload`; RabbitMQ
  `ssl:clear_pem_cache()`). It succeeds only when five consecutive new
  connections verify (code 0) and receive the new serial, within 60 attempts.
  A timeout fails the command, even if the last connection matched; the
  first version of this slice accepted that case, and review found it.
  Established connections keep the session they negotiated. `init` reissues
  a certificate within two days of expiry or when it does not verify for its
  name. `init` also replaces an internal CA whose constraints still permit
  subdomains, and `check-secrets` refuses one.
- **Ownership.** The lab operator owns both CAs and all certificates through
  `tools/security-lab-local`. Nothing renews automatically; there is no
  revocation.

## Consequences

- A certificate problem on PostgreSQL or RabbitMQ is an availability problem
  for **new** connections only. Established pooled or broker connections keep
  working until they close, so a bad certificate can go unnoticed until a
  restart, pool turnover, or reconnect. Measured in the experiment: the API
  answered 503 after a restart, and a restarted worker stayed disconnected
  with processing jobs left `accepted`.
- Recovery needs only a valid certificate and a reload: the API's pool and the
  worker's reconnect loop recovered without restarting either.
- Server-side logs do not always say why a client refused: Go clients sent
  `bad certificate` for every failure kind, and libpq sent no alert for a
  hostname mismatch. The client's log is the authoritative reason.
- pgx reads `sslrootcert` when the pool is created, so a new internal CA
  reaches the API and worker only after a restart. amqp091-go reads
  `cacertfile` on every dial (from its source; not exercised). CA rotation is
  not automated; a new CA requires `purge-state` and `init`, as for the lab CA.
- Labs created before this change need `init` (new CA, certificates,
  connection URLs) and `start` (recreates containers). Their data volumes are
  compatible because `pg_hba.conf` is read from the repository, not the
  volume. This upgrade path was **not exercised**; the original lab was not
  started or changed.
- Server keys are readable by any process inside their own container (as for
  the ingress). The migrator remains a full-capability container on `data`
  while it runs.

## Reconsideration conditions

- A plaintext network gains a third member, or an `app` member loses
  `cap_drop: ALL` or `no-new-privileges`: encrypt that network's hops.
- The lab spans hosts, or a datastore or the identity provider moves to a
  managed service: encrypt every hop, and treat the ingress → Keycloak hop
  first.
- Client keys can be hardware-backed or attested, or passwords become shared
  across workloads: reconsider mTLS (option C).
- Certificates are renewed by automation or a public or enterprise CA: replace
  the lab tool's issuance and reload steps and add expiry monitoring.

## Capability levels

No change.

## Later evidence

### 2026-10-13 — Internal TLS experiment

From the [internal TLS experiment](../experiments/issue-121-internal-tls.md),
run in a separate lab project (`zero-to-prod-security-lab-tls`) with scripted
clients only (`verify-internal-tls.json`: 75 pass, 30 observations, 0 fail):

- **Baseline:** every API and worker connection used TLS 1.3; libpq refused
  the lab CA for the internal server; PostgreSQL rejected `sslmode=disable`;
  RabbitMQ's port 5672 refused connections.
- **Failures** (untrusted issuer, wrong name, expired; on both services): new
  connections were refused by OpenSSL (codes 21, 62, 10), libpq, and the Go
  clients with the specific `x509` reason; established connections continued
  and carried complete create-and-process flows; a restarted API answered 503
  and a restarted worker stayed disconnected with jobs `accepted`.
- **Recovery:** renewal and reload restored new connections; the API and
  worker recovered without restarts and pending jobs completed.
- **Regression:** `verify`, `verify-failures`, `verify-realm`,
  `verify-authentication`, `verify-log-hygiene`, and `verify-authorization`
  passed in the same lab.

### 2026-10-13 — Review fixes (same PR)

- **CA names:** the first version permitted the DNS subtrees `postgres` and `rabbitmq`, so `extra.postgres` passed verification. The internal CA now also excludes `.postgres` and `.rabbitmq`.
  - In the rerun (`verify-internal-tls-rerun.json`: 94 passed, 35 observations, 0 failed), a certificate naming `postgres` and `extra.postgres` was refused by OpenSSL (code 48), libpq, and the Go clients (`… is excluded by constraint ".postgres"`); likewise for `rabbitmq`.
  - Established connections continued, and renewal recovered.
- **Renewal confirmation:** a timeout no longer counts as success. Renewal requires five consecutive verified connections with the new serial; regression cases cover a single final match, intermittent matches, and no match.

Open:

- the plaintext hops, including ingress → Keycloak;
- mTLS;
- automated renewal and monitoring;
- the upgrade of existing labs;
- verifiers other than OpenSSL and Go;
- single runs.

The decision stays Proposed pending review.
