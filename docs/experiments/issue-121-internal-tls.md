# Issue #121 — internal TLS and certificate failure behavior

Recorded 2026-10-13. This record covers the internal transport slice decided
in [ADR 0006](../adr/0006-security-lab-internal-transport.md) (Proposed):
server-verified TLS from a separate internal CA on the shared data network,
with PostgreSQL and RabbitMQ refusing plaintext. It builds on the merged
authorization baseline (#135; [ADR 0004](../adr/0004-security-lab-authentication.md)
and [ADR 0005](../adr/0005-security-lab-authorization.md), both unchanged).
It does **not** complete #121: the remaining internal hops, rate limiting, and
secret rotation are open.

## Question

Which internal connections should be encrypted, who is trusted on each, and
what happens to new and established connections when a certificate is
untrusted, names the wrong host, or has expired? Can legitimate traffic
recover without rebuilding the lab?

Environment classification: **LOCAL-FIRST**, in a separate security lab
project on one workstation. TLS verification and failure semantics are
portable; network membership and capability measurements are specific to
Docker Desktop on this machine.

## Environment

| Component | Version or mode |
| --- | --- |
| Host | Windows 11 Pro (build 26300), WSL 2 in NAT mode, Docker Desktop (Engine 29.6.2, Compose 5.3.1 as in earlier records; not re-recorded) |
| PostgreSQL | 18.6 (Alpine image, digest-pinned) |
| RabbitMQ | 4.3.6 (digest-pinned), Erlang TLS with OpenSSL 3.5.8 (from its startup log) |
| Clients | API and worker built with Go 1.27.1; pgx v5.11.0; amqp091-go v1.15.0 |
| Probes | OpenSSL 3.5.8 `s_client` (RabbitMQ image); `psql`/libpq from the PostgreSQL 18.6 image; WSL OpenSSL 3.5.5 for issuing |

## Setup

- **Separate lab.** Every run used the project
  `zero-to-prod-security-lab-tls`, with its own state directory, lab CA,
  internal CA, realm, and volumes. The original `zero-to-prod-security-lab`
  project was not started or changed; its four volumes
  (`_postgres_data`, `_rabbitmq_data`, `_identity_postgres_data`,
  `_keycloak_postgres_data`) and its state directory were left alone, as were
  the `-authz` project and the development volumes (see
  [cleanup](#cleanup-and-final-state)).
- **Method.** Scripted clients only: curl with cookie jars for Work Items,
  OpenSSL and libpq probe containers on the data network, the API and worker
  themselves as the Go clients, and container logs. No browser check was
  made in this slice; internal TLS is not visible to a browser.
- **Evidence provenance.** The evidence was produced from the working tree
  before commit, with these blobs (`git hash-object`), which are the ones
  committed:

  | File | Blob |
  | --- | --- |
  | `tools/security-lab-local` | `c93d6dfffb685301309665bc7441fa47ef1954bb` |
  | `infra/security-lab/compose.yaml` | `4908e8d5edb7f8e28c6c3f1f5f4a4bd7c12c0c34` |
  | `infra/security-lab/postgres/tls.sh` | `d06d7a72a6815554452b0f220c9577f0d4b2aa88` |
  | `infra/security-lab/postgres/pg_hba.conf` | `9146c3698f4e8399b35a664d5313a30e06325db1` |
  | `infra/security-lab/rabbitmq/rabbitmq.conf` | `94880902eafc97a01f448e38a35c80b5200264c9` |

  Before the evidence runs this project's containers, networks, and volumes
  were destroyed and recreated by `start`; its state directory (CAs,
  certificates, secrets) was kept from the development runs the same day.

## Threat model and decision

The [threat model](../architecture/security-lab-threat-model.md) maps every
internal connection, the members of its network, and what crosses it. ADR 0006
compares plaintext on private networks, server-authenticated TLS, and mTLS,
and chooses server-authenticated TLS for the `data` network only. The
network inventory measured in this experiment is the basis for that split:

| Container | Networks | PID 1 user | `CAP_NET_RAW` effective / bounding | `no-new-privileges` |
| --- | --- | --- | --- | --- |
| postgres | data | 70 | 0 / 1 | no |
| rabbitmq | data | 999 | 0 / 1 | no |
| api | app, data | 100 | 0 / 0 | yes |
| worker | data | 100 | 0 / 0 | yes |
| migrate (on demand; from the Compose model) | data | image default | no `cap_drop` | no |
| ingress | app, edge, idp | 101 | 0 / 0 | yes |
| oauth2-proxy (no `/proc` reader; from its configuration) | app, session | 65532 | `cap_drop: ALL` | yes |
| keycloak | idp, idp-data | 1000 | 0 / 1 | no |
| keycloak-postgres | idp-data | 70 | 0 / 1 | no |
| valkey | session | 0 | 1 / 1 | no |

`data` is the only network with more than two members where some member can
gain raw sockets; on `app` every member has dropped them. Valkey's PID 1 runs
as root with raw sockets, but its network has one other member, the gateway.

## What was built

- **Internal CA** (`init`): separate from the lab CA, ECDSA P-256, 90 days,
  `pathlen:0`, name-constrained to `DNS:postgres` and `DNS:rabbitmq`, IPs
  excluded. Its key never leaves `pki/` (mode 600).
- **Server certificates**: one per datastore, 14 days, the service name as
  the only SAN, `tls.pem` (key and certificate) in `internal-tls/<service>/`,
  mounted read-only; `init` reissues near expiry or when the name does not
  verify. `issue-internal-certificate <service>` renews and reloads.
- **PostgreSQL**: `ssl=on`, TLS 1.2 minimum, repository `pg_hba.conf`
  (`hostssl` with SCRAM, `hostnossl` rejected), and an entrypoint that copies
  the key into a tmpfs owned by `postgres` (`postgres/tls.sh`).
- **RabbitMQ**: TLS listener on 5671 only (`listeners.tcp = none`), TLS
  1.2/1.3, no client certificates.
- **Clients**: generated URLs with `sslmode=verify-full&sslrootcert=…` and
  `amqps://…?cacertfile=…`; the internal CA certificate is mounted into the
  API, worker, and migrator. No application code changed.
- **Preflight**: `check-secrets` refuses a missing, expired, wrong-name, or
  mismatched internal certificate and any connection URL that does not
  require verified TLS.
- **Experiment**: `verify-internal-tls`; **CI**:
  `scripts/test-security-lab-internal-tls.sh` (CA constraints, separation
  from the lab CA, names, URLs, and preflight refusals; no Docker).

## Results

Evidence: [`evidence/issue-121/internal-tls/`](../../evidence/issue-121/internal-tls).
All runs on 2026-10-13 between 20:04 and 20:25 UTC.

| Check | Result |
| --- | --- |
| `verify-internal-tls` (`verify-internal-tls.json`) | 75 pass, 30 observations, 0 fail |
| `verify` (`verify.json`) | 82 pass, 4 observations, 0 fail; includes the new 5671 probes and the refused plaintext port 5672 on the data network. The Windows "no lab CA" control ran (this project's CA is not trusted by Windows). The Windows LAN address is redacted |
| `verify-failures` (`missing-secret-failures.json`) | 14 / 14, including four new cases: missing PostgreSQL TLS file (preflight and container), plaintext database URL, plaintext AMQP URL |
| `verify-renewal`, ingress (`ingress-certificate-renewal.json`) | pass |
| `verify-realm` (`verify-realm.json`) | 11 / 11 |
| `verify-authentication` (`verify-authentication.json`) | 77 pass, 10 observations, 0 fail |
| `verify-log-hygiene` (`log-hygiene.json`) | 17 pass, 21 observations, 0 fail |
| `verify-authorization` (`verify-authorization.json`) | 79 pass, 14 observations, 0 fail; the pinned pre-authorization API (`0682b52`) also ran over the TLS URLs, unchanged |
| `scripts/test-security-lab-internal-tls.sh` | 23 / 23 (local; also in CI) |
| State-safety, startup-warning, CI-workflow, and classifier tests; Compose config; actionlint; `go test ./...` | pass (local) |

The authentication, authorization, log-hygiene, and reachability counts match
the authorization slice's runs (#135) except for the three new reachability
checks, so no regression was detected by them.

### Baseline

| Hypothesis | Observed |
| --- | --- |
| New connections verify against the internal CA for the service name | OpenSSL verify code 0 and the file's serial on both services |
| libpq with `verify-full` accepts the server | `fe_sendauth: no password supplied`: TLS and server identity accepted, then authentication requested |
| The lab CA (browser-facing) cannot vouch for an internal server | libpq `verify-full` with the lab CA: `SSL error: certificate verify failed` |
| PostgreSQL refuses plaintext even when the client asks for it | `sslmode=disable`: `pg_hba.conf rejects connection … no encryption` |
| RabbitMQ has no plaintext listener | 5672 from the data network: `refused connects=0` |
| Every live workload connection uses TLS | PostgreSQL: the API and worker backends, `ssl=true`, TLSv1.3; RabbitMQ: both connections `ssl=true`, tlsv1.3; zero without TLS |
| Work Items still works | Create and process: `succeeded` |

### Failure cases

For each service and each kind, the tool installed the bad certificate,
reloaded the service, waited for five consecutive new connections serving it,
and then observed:

| Service | Kind | New connection (OpenSSL code; libpq) | Established connections | New workload connection (Go client log) | Server log | Recovery after renewal, no client restart |
| --- | --- | --- | --- | --- | --- | --- |
| PostgreSQL | Untrusted issuer | 21; `certificate verify failed` | Kept; create and process `succeeded` with 0 new backends | Restarted API: 503; `x509: certificate signed by unknown authority` | `tlsv1 alert unknown ca` (libpq); `bad certificate` (Go) | API 200, create and process `succeeded` |
| PostgreSQL | Hostname mismatch | 62; `server certificate for "rabbitmq" does not match host name "postgres"` | Kept; `succeeded`, 0 new backends | 503; `x509: certificate is valid for rabbitmq, not postgres` | none (libpq); `bad certificate` (Go) | API 200, `succeeded` |
| PostgreSQL | Expired | 10; `certificate verify failed` | Kept; `succeeded`, 0 new backends | 503; `x509: certificate has expired or is not yet valid` | `certificate expired` (libpq); `bad certificate` (Go) | API 200, `succeeded` |
| RabbitMQ | Untrusted issuer | 21 | Kept; `succeeded`, 0 new connections | Restarted worker: not connected; `x509: certificate signed by unknown authority`; a new job stayed `accepted` | `CLIENT ALERT: Fatal - Bad Certificate` | Worker reconnected by itself; pending job and a new one `succeeded` |
| RabbitMQ | Hostname mismatch | 62 | Kept; `succeeded` | Not connected; `x509: certificate is valid for postgres, not rabbitmq`; job `accepted` | `Bad Certificate` | Reconnected; both jobs `succeeded` |
| RabbitMQ | Expired | 10 | Kept; `succeeded` | Not connected; `x509: certificate has expired or is not yet valid`; job `accepted` | `Bad Certificate` | Reconnected; both jobs `succeeded` |

A request that succeeded while new connections were failing must have used
an established connection; the connection counts before and after each flow
confirm it.

### Renewal

For each service a valid certificate was renewed on the running lab: new
connections verified the new serial (code 0, serial changed), every
established connection survived, and create and process `succeeded`.
`issue-internal-certificate` performs the same steps.

## Observations

1. **Established connections hide certificate problems.** Pooled PostgreSQL
   connections and the long-lived AMQP connections kept working under every
   bad certificate. A failure surfaces only on a new connection: a restart,
   pool turnover, or a reconnect after a broker or network fault.
2. **The client knows why; the server often does not.** Go clients sent the
   generic `bad certificate` alert for all three kinds, and libpq sent none
   for a name mismatch (it checks the name after the handshake). Server logs
   prove a rejection happened, not its cause.
3. **The API and worker recover without restarts.** pgx retries on the next
   request; the worker's session loop retries every second. Neither needed a
   restart after renewal.
4. **OpenSSL completes the handshake despite a failed verification** unless
   told otherwise, which is why the probe reports a verify code and the
   served serial, and why the libpq and Go clients are the enforcement
   evidence.
5. **PostgreSQL's key ownership rule** required copying the key into a tmpfs
   at start and on renewal; a bind-mounted key owned by the host user is
   refused.
6. **During development**, the first run aborted on a log search that found
   nothing (`pipefail`) after a mismatched certificate was installed. The
   exit trap reissued and reloaded a valid certificate; the next run's
   baseline verified the replacement serial. The search was fixed before the
   evidence runs.

## What this does not prove

- Anything about the hops left plaintext (ingress → API, oauth2-proxy,
  Keycloak; oauth2-proxy → Valkey; Keycloak → its database).
- That a member of the data network could actually capture or spoof traffic
  before this change; the inventory shows the capability, no attack was run.
- The migrator's own server verification: it connected over TLS (plaintext is
  rejected), but no failure case was run against it.
- CA rotation, revocation, or automated renewal; behavior at real expiry of a
  running certificate (simulated with a certificate issued already expired).
- The upgrade of a lab created before this change; the original and `-authz`
  labs were not started.
- Browser behavior: no manual browser check was made in this slice.

## Remaining gaps

- Plaintext internal hops, with ADR 0006's reconsideration conditions.
- No client certificates; client identity remains passwords in mounted files.
- PostgreSQL, RabbitMQ, and the migrator keep default capabilities on the
  data network.
- Manual renewal only, no expiry monitoring, no revocation; established
  connections mask failures.
- Secret rotation, rate limiting, observability, availability, and restore
  (#130, #131) are unchanged.

## Decision and capability levels

[ADR 0006](../adr/0006-security-lab-internal-transport.md) records the
decision as Proposed. No capability level changes.

## Cleanup and final state

The `zero-to-prod-security-lab-tls` project is kept for review with its
volumes and state directory; it was stopped after the runs. The untrusted
test CA existed only in a temporary directory during each run. The original
`zero-to-prod-security-lab` (four volumes), the `-authz` project (three
volumes), and the development volumes kept their names and creation times
and were not started; their contents were not inspected.
