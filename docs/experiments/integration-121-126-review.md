# Integration review: security line (#139–#141) with the operations line (#140, #142–#146)

- Date: 2026-10-18
- Branch: `review/integration-121-126` = #146 head `728426b` merged with #141
  head `5f9919d`, plus the fixes below
- Evidence: [`evidence/integration-121-126/run1/`](../../evidence/integration-121-126/run1/)
- Lab: a new, isolated security lab project `zero-to-prod-security-lab-integ`
  (subnets 10.213.30.0/24–10.213.37.0/24, checked free; no retained project
  touched)

## Why

The draft PRs form two stacks that both start from `main`: the security line
(#139 → #141) and the recovery/observability/capacity/availability/delivery/
operations line (#140 → #142 → … → #146). Each lab passed on its own branch.
That does not show the two lines combine: a reviewer merging both would get
code no lab had run. This review merges them, runs the security lab's own
verifications against images built from the merged code, adds one combined
check for telemetry under the security controls, and lists what still has
not been combined.

## Results

| Check | Result on the merged code |
| --- | --- |
| Git merge | **conflict** in `apps/work-items/api/main.go` (TLS listener vs metrics/structured startup log); resolved by keeping both |
| `go build`, `go vet`, `go test ./...` | pass |
| `verify` (ingress, reachability, published ports, Windows host) | 91 checks pass (rerun in the foreground; see note 1) |
| `verify-internal-tls` (PostgreSQL/RabbitMQ verified TLS, failure modes, renewal) | first run **2 failures**, a real integration defect (D2); 0 failures after the fix |
| `verify-app-tls` (ingress → API/oauth2-proxy TLS) | 257 checks pass; rerun exit 0 (see note 2) |
| `verify-authorization --skip-slow` (permission matrix, 401/403, logs) | first run: 51 pass, then the pinned pre-authorization control image failed to build (Docker Desktop credential helper); rerun: 52 pass, then the control **failed**, a real integration defect (D3); rerun 2 after the fix: 74 checks pass |
| `verify-authentication --skip-slow` | pass |
| `verify-log-hygiene` (codes, cookies, tokens, secrets in logs) | pass |
| `test-security-lab-secret-rotation.sh` (#141 rotation with injected failure and resume) | pass |
| New combined check `tools/integration-security-telemetry-check` | 9/9 pass (below) |

Migration 000007 (`outbox_messages.trace_context`) was applied by the
security lab's own migrate step against its TLS-only PostgreSQL; the
security lab therefore ran the operations line's schema.

### Combined telemetry check

With OIDC authentication, the HTTPS ingress, TLS to the API, and verified TLS
to PostgreSQL and RabbitMQ all on, and the #122 metrics listeners enabled on
each container's loopback only (no new network hop), one machine-client
token:

- an anonymous request was refused (401, by the ingress), and a request
  without the permission was refused by the API (403) and counted as
  `work_items_http_requests_total{route="/items",code="403"}`;
- `POST /api/items/{id}/process` was accepted (202); the outbox row stored
  `trace_context`; the API's `processing accepted` record and the worker's
  `delivery settled` record carry the same `trace_id`, so trace propagation
  works over AMQPS with the production RabbitMQ permissions;
- the job succeeded; requests were logged and counted by route pattern;
- none of 32 secret values (every generated password and client secret, raw
  and base64) nor the access token or its signature, nor any `Bearer <token>`
  pattern, appeared in the API or worker logs or metrics. This is a
  known-value scan with the access token as the positive target, not a
  universal secret detector.

The script restores the API and worker to the lab's own configuration on
exit (it does so on failure too; an earlier trial without that trap left the
override in place until the next run).

## Defects found and fixed in the integration branch

**D1 — merge conflict in `api/main.go`.** #141 adds an optional TLS listener
and an `(HTTP|HTTPS)` startup line; #142 adds the metrics listener and
structured startup log. Resolution keeps both:
`slog.Info("work-items-api listening", "address", …, "scheme", …)`. No tool
or test matched the old text.

**D2 — security verification broken by JSON logs.** #142 (ADR 0012) changed
the API and worker logs to JSON. `security-lab-local`'s `go_tls_reason`
extracted Go TLS errors from the logs with a regular expression containing
literal quotes (`DNS name "extra.postgres" is excluded by constraint
".postgres"`); JSON escapes them as `\"`, so the two name-constraint checks
of `verify-internal-tls` observed nothing and failed. The certificates were
rejected correctly — only the verification lost sight of the reason. The fix
unescapes `\"` before matching, so plain-text and JSON logs give the same
result. Other log checks in the tool match substrings without quotes
(`authorization denied`, `holds no valid key pair`) and passed.

**D3 — authorization control incompatible with the delivery protocol.**
`verify-authorization` compares the current API with an API built from the
pinned pre-authorization commit `0682b52`, which must accept every request.
That commit predates #140's version-2 messages with delivery identity
(ADR 0009). In the combined tree the worker rejects its version-1 deliveries,
as designed, so the control's fixture job stayed `accepted` (job 63: payload
version 1, no `delivery_id`) and the control stopped with `fixture job did
not succeed`. The control tests authorization, so the fix expects
`accepted` for baseline-mode jobs (the call was accepted) and keeps
`succeeded` for the current API. Rerun 2 passed all 74 checks. The rejected
baseline jobs remain `accepted` in this disposable lab's database; with
telemetry enabled they would trip the stalled-processing alert, which is the
intended symptom.

None of the three defects exists on either branch alone; all appear only
when the lines are combined, which is the point of this review.

## Notes on the run

1. The first `verify` passed every check up to the Windows-host section, then
   stopped at `wslpath`, which reported the existing CA file missing. The
   same command passed all 91 checks when run in the foreground: WSL interop
   was unavailable to the backgrounded session, not a product failure. Both
   logs are kept.
2. `verify-app-tls` in the first sequence passed all 257 checks and wrote its
   evidence, then exited 2 with `syntax error near unexpected token ';;'`:
   the reviewer edited `security-lab-local` (fix D2) while that bash process
   was still reading the file. The command was rerun after the edit. Bash
   reads scripts incrementally; editing a running script is unsafe.
3. The security lab builds images under fixed tags shared by every security
   lab project (`zero-to-prod-security-lab-{api,worker,ingress}:local`). This
   run rebuilt them from the merged code, so a retained security lab started
   later with `start` (which rebuilds anyway) or `up --no-build` would use
   these images. No retained container, volume, or network was changed.
4. All logs, JSON evidence, and the hashes of the tool and check versions
   used for the reruns (`rerun-provenance.txt`) are kept unchanged.

## Not combined, and why it matters (static review)

These were found by reading the branches together; they are not tested and
are listed so nobody infers a combined guarantee from separate labs.

| # | Gap | Evidence | Consequence |
| --- | --- | --- | --- |
| G1 | **HA routing vs internal TLS names.** The availability lab (#124) routes clients through an nginx stream proxy named `proxies` to `postgres`, a standby, and `rabbitmq`/`rabbit2`/`rabbit3`, all plaintext. The internal CA (#121, ADR 0006) is name-constrained to exactly `postgres` and `rabbitmq`. | `infra/availability-lab/proxies.conf`; `INTERNAL_TLS_SERVICES=(postgres rabbitmq)` | A combined HA + TLS profile needs a naming and certificate design (clients keep verifying `postgres` through a TCP-passthrough proxy alias, standby and extra broker nodes get certificates, the CA constraint is extended deliberately). Not exercised; failover with `verify-full` is unproven. |
| G2 | **Queue type.** The security lab's definitions declare `work_item_processing` as a classic queue; #124's broker HA depends on a three-member quorum queue. The application does not declare the queue. | `tools/security-lab-local` definitions (`"arguments": {}`); `tools/availability-lab-local` (`x-queue-type: quorum`) | A queue's type cannot be changed in place. A combined deployment must choose quorum up front or plan a drain-and-recreate migration. Broker-node loss in the security profile would lose the queue's availability. |
| G3 | **Ingress admission vs capacity.** The security ingress limits the API to 20 r/s per client address (burst 40) and 100 r/s in total (ADR 0010). #123's single-API baseline and ramps were measured at 60–360 r/s from one client without that ingress; its admission stage reused only the per-IP rule in a plaintext gateway. | `infra/security-lab/ingress/nginx.conf`; `docs/experiments/issue-123-capacity.md` | Behind the real ingress, one client or NAT is capped far below the measured capacity, and 429s are invisible to the API's availability SLI (#123 already observed the second point). Capacity and SLOs of the combined profile are unmeasured. |
| G4 | **Telemetry hops are outside the security model.** OTLP export to the collector and Prometheus scraping are plaintext in the observability, capacity, availability, delivery, and operations labs; the security threat model does not list them. The combined check above kept the metrics listeners on loopback to avoid adding a hop. | `infra/*-lab/compose.yaml`; `docs/architecture/security-lab-threat-model.md` (no telemetry connections); `security-lab-security-model.md` names new telemetry only as future work that must keep credential hygiene and bounded labels | Enabling telemetry in the security profile adds connections that carry identifiers and timing (not tokens, per the scan) and need a transport decision. |
| G5 | **Multiple API instances vs single-service security tooling.** #124 runs two APIs and #125 blue/green colours; the application CA issues for the name `api`, and `security-lab-rotate-app-db` recreates exactly the `api` service. | `APP_TLS_SERVICES=(api oauth2-proxy)`; rotation tool | Rotating the application database password or renewing certificates with several API instances or two colours is untested; a rollout or failover could keep an instance on the old credential. |
| G6 | **Image gates cover only the API and worker.** #125's CI scans the API and worker images. The security boundary's images (custom nginx ingress, oauth2-proxy, Keycloak, Valkey) and the PostgreSQL/RabbitMQ images are pinned by digest but not scanned. | `.github/workflows/work-items-ci.yml` (`delivery-image-gate` for two images) | Vulnerabilities in the components that hold sessions, tokens, and data are not detected by any gate. The local scan below found HIGH or CRITICAL findings in five of six such images. |
| G7 | **Operations alerting is not wired to the security profile.** #126's Alertmanager, receiver, and drill alerts run in the operations lab with authentication off. | `infra/operations-lab/` | Incident detection for the authenticated profile (including auth-provider and ingress failures) is untested. |

## Image scan of the ungated images (G6)

The same gate #125 uses for the API and worker (`tools/delivery-image-gate`,
pinned Trivy, HIGH/CRITICAL fails; vulnerability database updated
2026-10-18 13:11 UTC) was run locally on the security lab's other images.
Evidence: [`evidence/integration-121-126/image-scan/`](../../evidence/integration-121-126/image-scan/)
(SBOM, findings, scanner provenance per image).

| Image (pinned) | HIGH | CRITICAL | Packages | Fixed version available |
| --- | ---: | ---: | --- | --- |
| ingress (custom, `nginx:1.30.5-alpine` base) | 4 | 0 | libexpat ×2, pcre2, tiff (Alpine 3.24.2) | yes |
| `oauth2-proxy:v7.15.5` | 4 | 0 | Go stdlib ×3, golang.org/x/net | yes |
| `keycloak:26.8.0` | 6 | 0 | pcre2, pcre2-syntax | yes |
| `postgres:18.6-alpine3.24` | 24 | 1 | Go stdlib (a bundled Go binary, not PostgreSQL itself) | yes |
| `rabbitmq:4.3.6-management` | 2 | 0 | OpenSSL | yes |
| `valkey:9.1.2-alpine` | 0 | 0 | — | — |

The PostgreSQL and RabbitMQ images are used by every lab on both lines, so
this gap is not specific to the security profile. Reachability was not
analyzed: an image finding is a candidate, not proof of exploitability.

The ingress findings come from its base image. The newest upstream
`nginx:1.30.6-alpine` (scanned too) fixes pcre2, tiff, and one libexpat CVE
but still ships libexpat 2.8.5; the remaining CVE needs 2.9.0. A base bump
therefore reduces the ingress to one HIGH finding, not zero. It was not
applied here, because it would change the image every #121 security
experiment ran against; it is a decision for #141 (bump and rerun its
verifications, or accept and track the residual finding). The first
attempt at five of these scans crashed in the gate's path resolution (a
relative output directory on the `/mnt/c` mount); the logs are kept as
`*.attempt1-crash.log` and the rerun used absolute paths.

## Issue reconciliation against acceptance criteria

Every criterion was compared with the drafts' code, documents, and evidence.
Criteria not listed are met within the agreed local scope by the cited PR's
recorded evidence. "Partly" means met locally with a stated limit; "not met"
means no evidence yet. No issue is ready to close while its PRs are
unreviewed drafts.

| Issue | Criterion | Status | Reason / remaining local work |
| --- | --- | --- | --- |
| #121 | Threat model for important boundaries and assets | partly | Transport threat model plus #141's security model; telemetry connections (G4) and the HA/delivery topologies (G1, G5) are not modeled |
| #121 | Security-sensitive logs do not expose credentials | partly → strengthened | `verify-log-hygiene` and #141; this review adds the JSON logs and metrics of the merged code (known-value scan, 32 secrets, token as target) |
| #121 | Remaining production security gaps explicit | partly | #141 lists two plaintext hops and CA/secret lifecycles; G1–G6 above add the combined-profile gaps |
| #122 | Logs, metrics, traces, SLOs, alerts | met (local) | Merged-code check confirms propagation under TLS and auth; telemetry transport security is G4 |
| #123 | Rate limiting and overload compared | partly | Compared in a plaintext gateway with the per-IP rule only; the real ingress's total limit and TLS are untested (G3) |
| #123 | Capacity tied to SLOs; no claim beyond tested range | met (local) | Capacity of the authenticated combined profile is unmeasured (G3) |
| #124 | Off-site backup semantics | partly | Separate volume/transfer boundary on the same disk; real off-site survival unproven (stated in the PR) |
| #124 | Broker-node failure does not silently lose acknowledged guarantees | partly | Process kill/stop and majority loss exercised; permanent disk loss and partitions not; quorum queue absent from the security profile (G2) |
| #124 | No HA claim beyond tested failure domains | met | One host; failover with verified TLS untested (G1) |
| #125 | CI preserves security and correctness gates | partly | Gates cover API and worker images only; five ungated images have HIGH/CRITICAL findings (G6) |
| #125 | Dependency vulnerabilities automatically detected | partly | Same scope limit as above |
| #125 | Deployment under active traffic, rollback | met (local) | Authentication off; an authenticated rollout through the TLS ingress with two colours is untested (G5) |
| #126 | On-call expectations and escalation understood | partly | Single owner, supervised only, no secondary or escalation (owner-confirmed) |
| #126 | Runbooks tested; detection and recovery measured | partly | Scripted drills measured; notification visibility shown; independent human diagnosis and chosen action not shown, so no human MTTA |
| #126 | Reliability under sustained traffic vs SLOs | met as measured, **missed** | 89.16 % availability and 97.27 % freshness in the fault window, below 99.5 % and 99 %; recovered subset met them. A measured miss, correctly reported, not a pass |
| #126 | Final verdict distinguishes implemented / tested / unproven | met | Verdict: controlled local experimentation only |
| #120 | Capability map and readiness verdict reconciled | partly | Depends on the above; this review supplies the combined-profile gaps |
| #130, #131 | All criteria | met (local) | #140 evidence; no integration conflict with the security line |

## What this review does not change

No pull request was merged, retargeted, or closed, and no issue was closed.
The draft stacks stay as they are; this branch exists so the combined state
can be reviewed and CI-tested. The readiness verdict in
[issue-126-readiness-review.md](issue-126-readiness-review.md) stands:
controlled local experimentation only.
