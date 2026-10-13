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
  `pathlen:0`, permitted `DNS:postgres` and `DNS:rabbitmq`, IPs excluded.
  Its key never leaves `pki/` (mode 600). *As first built (and in the first
  evidence run) the permitted constraints also allowed subdomains; see
  [review fixes](#review-fixes-and-rerun) for the exact-name policy now in
  place.*
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
First evidence run on 2026-10-13 between 20:04 and 20:25 UTC, at the PR's
first version. The [rerun after review](#review-fixes-and-rerun) supersedes
parts of it.

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
| `scripts/test-security-lab-internal-tls.sh` | 23 / 23 at the first version (local; also in CI); 42 / 42 after the review fixes |
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
   generic `bad certificate` alert for all three kinds (and for the
   subdomain case in the rerun), and libpq sent none
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

## Review fixes and rerun

Review of PR #136 at `5b9fbe0` found two defects. Both were fixed in this
PR, and the affected runtime experiments were rerun. The results above are
the first evidence run and stay as recorded. The rerun supersedes them where
noted below.

| Finding | Effect on the first run | Fix |
| --- | --- | --- |
| The internal CA permitted `DNS:postgres` and `DNS:rabbitmq`. A permitted dNSName constraint covers the whole subtree, so the reviewer's certificates for `extra.postgres` and `extra.rabbitmq` passed OpenSSL chain and hostname verification (`keycloak` was refused) | "Permits only postgres and rabbitmq" (CI test label) and "cannot vouch for any other name" (ADR 0006) overstated the guarantee. The first run's failure cases are unaffected: none of them depended on that claim | **Exact names enforced.** The constraints also exclude `DNS:.postgres` and `DNS:.rabbitmq`. `check-secrets` refuses an internal CA without the exclusions; `init` replaces it and reissues both certificates. New `subdomain` failure case: a certificate naming `postgres` (or `rabbitmq`) **and** `extra.<service>` is internal-CA-signed and matches the dialed name, so only the exclusion can refuse it |
| `wait_for_internal_serial` returned its last observation successfully after 60 attempts, even without five consecutive matches. 59 old-serial observations followed by one new one returned `0 NEW`, which the renewal caller accepted | A renewal or failure-case step could have passed on one matching connection. The first run's renewal and failure-case observations are not known to be wrong, but they did not have this guarantee | **Timeout fails.** The rule is five consecutive observations equal to the full expected value (verify code and serial) within 60 attempts. Otherwise the command fails and prints `timeout: <streak> of 5 …; last '<observation>'`, which can never equal the expected value. `issue-internal-certificate` exits non-zero on a timeout. Failure cases wait for their own nonzero code |

**Regression tests** (`scripts/test-security-lab-internal-tls.sh`, 42 checks, run locally and in CI):

- **CA policy, against the generated internal CA:**
  - `postgres` and `rabbitmq` accepted for their dialed names;
  - `extra.postgres` and `extra.rabbitmq` refused (`excluded subtree violation`);
  - `postgres` plus `extra.postgres` refused although the dialed name matches;
  - `keycloak` refused (`permitted subtree violation`);
  - a valid `rabbitmq` certificate refused for the dialed name `postgres` (`hostname mismatch`, the per-client name check);
  - a control CA with only the permitted subtrees accepts `extra.postgres`;
  - refusal and replacement of such a CA by `check-secrets` and `init`.
- **Renewal confirmation:** through the tool's `check-serial-streak` hook, which uses the same function as `issue-internal-certificate`:
  - five consecutive matches succeed, also after old observations;
  - intermittent matches (4, old, 4) fail;
  - a failed verification with the right serial resets the streak;
  - a single match on the final attempt (the reviewer's reproduction) fails;
  - no match before timeout fails;
  - a timeout never prints the expected value;
  - five matching nonzero-code observations succeed for a failure case.

  A mutation restoring the old timeout behavior failed five of these cases, including the reproduction.

**Rerun** in the same isolated project. `init` refused, then replaced, the subtree-only internal CA from the first run and reissued both certificates. The project's containers and volumes were recreated by `start`; its lab CA and secrets were kept. Tool blob `b0a67502fd6bbdfedbc194f01a9206520550ff05`; test blob `de8d00a71a5ebb6b7ab1a10878f79ebd5a693ea7`; the Compose, `pg_hba.conf`, `tls.sh`, and RabbitMQ blobs are unchanged from the table above.

| Check | Passed | Observations | Failed | Supersedes |
| --- | --- | --- | --- | --- |
| `verify-internal-tls` (`verify-internal-tls-rerun.json`, 21:01–21:04 UTC) | 94 | 35 | 0 | `verify-internal-tls.json` for the CA policy and the confirmation rule |
| `verify-failures` (`missing-secret-failures-rerun.json`) | 14 | 0 | 0 | `missing-secret-failures.json` (same cases, current tool) |
| `verify` (`verify-rerun.json`; Windows LAN address redacted) | 82 | 4 | 0 | `verify.json` (same checks, current tool) |
| `issue-internal-certificate postgres`, `rabbitmq` (console output) | both confirmed five consecutive verified connections with the new serial | — | 0 | — |

New subdomain case in the rerun, for both services:

- OpenSSL verify code 48 (excluded subtree violation) with the bad serial;
- libpq `SSL error: certificate verify failed` (PostgreSQL), with the server logging `alert certificate unknown`;
- established connections kept, and create and process `succeeded` with 0 new connections;
- the restarted API (503) and the restarted worker (disconnected, job `accepted`) logged `x509: a root or intermediate certificate is not authorized to sign for this name: DNS name "extra.<service>" is excluded by constraint ".<service>"`;
- renewal recovered both without restarts.

The exclusion is therefore enforced by OpenSSL, libpq, and Go's `crypto/x509` as used by pgx and amqp091-go, not only by OpenSSL.

`verify-internal-tls-rerun-attempt1.json` is kept:

- It was the first rerun: 92 passed, 35 observations, 2 failed.
- Both failures were the tool's expected Go message. It lacked "to sign", which Go 1.27 includes.
- Clients and lab outcomes were the same as in the final rerun: 503, worker absent, and the same error found in the container logs.
- The expected text was corrected, and the whole rerun was repeated on the final blob.

`verify-authentication`, `verify-log-hygiene`, `verify-realm`,
`verify-authorization`, and the ingress renewal were **not** rerun. The fixes
change neither their code paths nor their guarantees, and their first-run
results stand.

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
- How a verifier other than OpenSSL and Go reads the leading-dot exclusion.
  RFC 5280 leaves it undefined for dNSName, so other verifiers may differ.

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
