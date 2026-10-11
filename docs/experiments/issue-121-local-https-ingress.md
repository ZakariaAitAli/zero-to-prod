# Issue #121 — local HTTPS ingress and private backends (first slice)

Recorded 2026-10-10. This is the first implementation slice of Issue #121. It
does **not** complete #121: authentication, authorization, datastore TLS, rate
limiting, and secret rotation are later slices.

## Question

Can Work Items run behind one HTTPS entry point on host loopback, with private
backend services and runtime secrets that are not development defaults, and
can that exposure be measured rather than assumed?

Environment classification: **LOCAL-FIRST**. Network isolation, TLS
verification, and secret delivery are portable behaviors. The measurements
below come from one workstation; Docker and WSL networking results are
specific to it.

## Environment

| Component | Version or mode |
| --- | --- |
| Host | Windows 11 Pro (build 26300) |
| WSL | WSL 2, NAT networking mode, Ubuntu distribution, kernel 6.18.33.2-microsoft-standard-WSL2 |
| Docker | Docker Desktop with the WSL 2 backend; Docker Engine and CLI 29.6.2; Compose 5.3.1 |
| Ingress | nginx 1.30.5 (Alpine image, digest-pinned) |
| Probe clients | curl 8.22.0 (container image), WSL `curl`, Windows `curl.exe` (Schannel) |

The Docker Desktop application version was not recorded.

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

nginx and Caddy were compared from their documentation and, for nginx, the
contents of the image used here (`nginx -V` and its module directory). Caddy
was not implemented or run, so this is a provisional choice, not a measured
comparison.

| | nginx (official 1.30.5 Alpine image) | Caddy |
| --- | --- | --- |
| TLS with operator-supplied certificates | Yes | Yes |
| Automatic certificate management | Optional: the official `ngx_http_acme_module` ships in the image as a dynamic module; the lab does not load it | Enabled by default and configurable, including use of supplied certificates only |
| Static files, path allow-list, header replacement | Yes | Yes |
| Request limiting | `limit_req` built in | Third-party module |
| Subrequest authentication for a later session gateway | `auth_request` built in | `forward_auth` built in |

**Choice: nginx (provisional).** Both proxies can serve the lab's
operator-supplied certificates. nginx was selected for its explicit,
directive-level configuration of each boundary rule and for its built-in
request limiting, which the rate-limit slice needs. Revisit if automatic
issuance becomes the subject of an experiment or the rate-limit slice favors
a different design.

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
  certificate, and random runtime secrets in a validated, marked state
  directory outside the repository; checks them before start; starts,
  verifies, renews, stops, and destroys only this lab.
- nginx: TLS 1.2/1.3 for `work-items.localhost` only (other names fail the
  handshake), the built Web UI, an allow-list of three API routes, replacement
  of client forwarding headers, a 64 KiB body limit, and an access log of path
  without query string. Its key and certificate are one file in a mounted
  directory, so renewal reaches the container.

The structure is described in the
[security lab architecture](../architecture/security-lab.md) and operated
through the [security lab guide](../guides/local-security-lab.md).

## Defects corrected before merge

Three defects in the first version of this slice were identified after its
first evidence run on 2026-10-10; two of them were reproduced with disposable
checks. Each was fixed in a separate commit, and the affected experiments were
rerun from a freshly destroyed and restarted lab. The results below are from
that rerun. The first evidence run remains in Git history at commit `48f067d`.

| Defect | Effect on the first version | Fix and new evidence |
| --- | --- | --- |
| Certificate renewal replaced the mounted key and certificate by rename, but a single-file bind mount keeps the original inode | `issue-certificate` reloaded nginx with the old certificate, and the guide's renewal instructions were wrong. Renewal had not been exercised | Directory mount and a single combined file; renewal waits for five consecutive new connections with the new serial; [`certificate-renewal.json`](../../evidence/issue-121/certificate-renewal.json) |
| The reachability classifier read curl exit 28 as "no connection", but exit 28 also follows a successful connection whose response times out | An exposed service that delayed its response could have passed an isolation check. In the rerun every isolation probe still made no connection (`connects=0`), so no earlier conclusion changes | Classification from curl's connection count first; classifier controls with a listener that never responds, from a container, WSL, and Windows |
| `purge-state` recursively deleted any absolute state path outside the textual repository path | An override pointing at the home directory, or a symlink into the repository, would have been deleted | Canonical-path validation, allowed bases, an ownership marker, and refusal of unexpected entries; `scripts/test-security-lab-state-safety.sh` (33 cases, fixtures only) |

## Hypotheses and results

Automated evidence:
[`local-https-ingress-verification.json`](../../evidence/issue-121/local-https-ingress-verification.json)
(58 results: 53 pass, 4 observations, 1 skipped),
[`missing-secret-failures.json`](../../evidence/issue-121/missing-secret-failures.json)
(8 pass), and
[`certificate-renewal.json`](../../evidence/issue-121/certificate-renewal.json)
(pass). All were produced by `tools/security-lab-local` at commit `68d13fd`
on a freshly destroyed and restarted lab. The Windows LAN address and local
paths are redacted. Reachability results record curl's connection count and
exit code; "no connection" requires `connects=0`.

| Hypothesis | Observed |
| --- | --- |
| Only the ingress publishes a port, on 127.0.0.1 | Docker reports only `ingress=8443/tcp->127.0.0.1:9443`; Windows lists only `127.0.0.1:9443` among lab ports |
| A client verifying with the lab CA reaches the UI and API | WSL `curl` and Windows `curl.exe`: 200 with verification result 0 |
| Verification is actually enforced | Without the lab CA: curl exit 60 on WSL; another name: handshake rejected (exit 35). On Windows, exit 60 was observed in the first evidence run (`48f067d`). In the rerun the Windows control was skipped, with the reason recorded, because the lab CA was by then trusted in the Windows user store for the [manual browser verification](#manual-browser-verification) |
| The classifier detects a connection even without a response | A listener that accepts but never responds classified `connected connects=1 exit=28` from a container, WSL, and Windows; a closed port classified `refused connects=0 exit=7` |
| API operational endpoints are not exposed | `/api/health` and `/api/version`: 404 at the ingress |
| The ingress reaches the API internally | 200 through the ingress; `wget http://api:8080/health` inside the ingress |
| Host clients cannot reach backends directly | No listener on 8080, 5432, 55432, 5672, 15672 on WSL or Windows loopback; no connection on Windows non-loopback addresses or WSL `eth0` |
| An unrelated container cannot reach backends | `api` unresolvable; API, PostgreSQL, and RabbitMQ container IPs give `timeout connects=0` from an unrelated network, the default bridge, and the ingress's own client network |
| Those failures are not caused by stopped services | Same probe from the `app` network resolves `api` and connects to `api:8080`; from the `data` network it connects to PostgreSQL and RabbitMQ |
| Missing secrets fail clearly | Preflight, API (missing file, directory artifact, both URLs), worker (both URLs), ingress (certificate), PostgreSQL (admin password): all exit 1 with a message naming the input; no secret value in output |
| Work Items semantics unchanged through the ingress | Create 201; process 202; job `succeeded`, `attempt_count` 1; item `done` with result (26 characters, 4 words); second process `409 work_item_already_done` |
| Certificate renewal reaches new connections | Served serial changed with the file serial; the container's file matched the host file; control: a single-file bind mount still showed the original content after the host file was replaced |
| State cleanup cannot target unrelated directories | Rejected: relative path, `/`, home, an ancestor of home, the allowed base itself, paths outside the bases, the repository, symlink aliases to the repository and home, `..` escapes. Unmarked, foreign-marked, or extra-content directories survived `purge-state`; lab-created state was removed and its parent kept |

### Secret exposure checks

Run against the rerun lab instance after the processing check: each of the
six generated passwords was searched for in all container logs (322 lines),
`docker inspect` output for every lab container, `docker image inspect` and
`docker history` for the three lab images, the evidence files, and the Git
tree. No match. As a positive control, the same search found the app
password inside its own connection-URL file. Container environments contain
only `*_FILE` paths and no connection URLs.

### Development compatibility

Run in the temporary `zero-to-prod-121-compat` lab with the development tools
and labelled defaults, before the corrections above. The corrections change
only the security lab tooling, its Compose and nginx files, and CI policy; the
Go code and development tools are unchanged. After the corrections, `gofmt`,
`go vet`, `go test ./...`, the CI policy tests, the state-safety tests,
shellcheck, and actionlint were rerun and passed; the integration suites were
not rerun.

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

## Manual browser verification

Performed on 2026-10-10 in Brave on the Windows host against the running lab,
after the first automated evidence run. These observations were not captured
by the lab tooling, and no machine-readable record exists for them.

| Step | Observed | Evidence |
| --- | --- | --- |
| Open `https://work-items.localhost:9443/` before trusting the lab CA | `NET::ERR_CERT_AUTHORITY_INVALID` | Screenshot; not retained in the repository |
| Trust the lab CA in the Windows user store, then reload | Page loaded without a certificate warning | Manual observation |
| Create a Work Item titled "New Item" and process it | Item reached `done`; job `succeeded`; result 8 characters, 2 words (matches the title-analysis rule) | Manual observation |
| `./tools/security-lab-local stop` | Lab stopped | Manual observation; all five lab containers were afterwards listed as exited by `docker ps` |

Not verified in the browser: the certificate issuer displayed, the absence of
console errors, removal of the CA from the trust store, and access from
another device on the LAN.

## Unexpected behavior

Items 1–6 and 9–10 were observed in the environment above; they depend on
Docker, Docker Desktop, or WSL behavior and may differ elsewhere. Items 7, 8,
and 11 follow from general Linux bind-mount, nginx, and curl behavior.

1. **A missing Compose secret file does not fail.** With Compose 5.3.1
   outside Swarm, Docker created an empty directory at the mount point and the
   container started; `docker compose config` also passed. Fail-closed
   behavior therefore comes from the tool's preflight and from the
   applications rejecting a directory.
2. **Bind mounts kept the host owner and mode.** A mode-600 file owned by the
   WSL user was readable only by root or the same UID in containers. Mounted
   secret files are mode 644 inside a mode-700 directory instead.
3. **A loopback-only publication was still reachable from other containers.**
   An unrelated network and the default bridge connected to the ingress
   container's IP on 8443, and a verified HTTPS request succeeded that way.
   `host.docker.internal:9443` also connected. The container-loopback health
   port 8081 did not. Backends without published ports stayed unreachable.
   In this environment, any container on the same Docker engine can reach the
   ingress.
4. **Every host client appeared as one address.** The ingress logged
   `172.22.0.1` (the edge network gateway) for all WSL and Windows requests.
   Per-address limiting would treat all host clients as a single client.
5. **Unknown names were slow to fail.** From non-lab networks, `api` took
   about 4 seconds to return NXDOMAIN through the host resolver, longer than a
   3-second TCP timeout. BusyBox `nslookup` also exited 0 after printing
   NXDOMAIN. The resolution check uses curl's resolver with a longer timeout.
6. **Host-to-container-IP probes are weak evidence here.** From WSL, lab
   container IPs returned "refused" or "timeout" depending on the run; the
   Docker subnets (for example 172.20.0.0/16) overlap a Windows virtual
   adapter (172.20.208.1). The containers run in the Docker Desktop VM, so
   these probes say little; the container vantage points with positive
   controls and the listener tables are the primary evidence.
7. **A single-file bind mount pins the original file.** Replacing a mounted
   file by rename left the container reading the old inode. Directory mounts
   see the replacement.
8. **nginx serves the old certificate briefly after reload.** The new worker
   starts before the old one stops accepting; for about one second new
   connections could still receive the old certificate. Renewal therefore
   waits for consecutive matching connections.
9. **Rewriting secret files broke existing containers.** Rerunning `init`
   replaced the RabbitMQ definitions (new random salts) by rename, and the
   stopped broker container then failed to start with a missing mount source.
   `init` now keeps unchanged files and `start` recreates containers.
10. **Bind mounts kept directory modes too.** The mode-700 TLS directory
    blocked the ingress user (UID 101); it is mode 711 inside the mode-700
    state directory.
11. **curl exit 28 is ambiguous.** It occurs both for a connection timeout and
    for a response timeout after a successful connection.

## What this does not prove

- That the ingress is unreachable from another device on the LAN: no second
  device was used. Windows' own non-loopback addresses did not connect.
- Browser behavior beyond the
  [manual browser verification](#manual-browser-verification); the automated
  checks used `curl`, not a browser.
- Anything about authentication, authorization, CSRF, rate limiting, datastore
  TLS, certificate expiry behavior, or secret rotation.
- Isolation under a different Docker or WSL networking mode (for example WSL
  mirrored networking or Docker Engine without Docker Desktop).

## Remaining gaps

- No authentication or authorization; the lab must stay on loopback.
- Unencrypted internal hops (ingress → API, PostgreSQL, RabbitMQ).
- Ingress reachable from any container on the same Docker engine (item 3).
- Host client addresses collapse to the gateway (item 4).
- The native development API listens on all interfaces and is reachable from
  Windows through the WSL NAT address; unchanged here.
- No rotation, no revocation, single-tier CA, no CSP or HSTS, ingress image not
  built in CI, migrator URL in process arguments.
- #130 and #131 remain open and unmitigated; this slice does not touch restore,
  sequences, or message identity.

## Decision and capability levels

No ADR yet: the ingress choice is provisional, and the decisions #121 must
record (session model, authorization placement, transport policy) come in
later slices. This record changes no capability level; levels are assessed
against the baseline when #121's evidence is complete.

## Cleanup

The security lab is **kept** as scaffolding for the remaining #121 slices; its
exit decision is due when #121 closes. The temporary compatibility lab was
destroyed. The security lab was stopped after the manual browser verification;
for the rerun it was destroyed, started fresh, and stopped again, keeping its
volumes and state directory. At the rerun the lab CA was still trusted in the
Windows user store; the guide's
[removal steps](../guides/local-security-lab.md#removing-the-lab-ca) undo
that, and `destroy` and `purge-state --yes` remove the lab and its state.
