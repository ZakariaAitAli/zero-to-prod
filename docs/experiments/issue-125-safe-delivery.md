# Issue 125: local delivery and security maintenance

Refs #125. [Design](../adr/0015-safe-local-delivery.md),
[reproduction and boundaries](../guides/local-delivery-lab.md).
Evidence lives in `evidence/issue-125/`.

## Observations

The dedicated blue/green lab uses recorded Docker image IDs, one stable gateway,
and the existing database/broker. Runtime secrets stay outside Git. Containers
were stopped after each run; state, images, network and volumes are retained.

- Run 1 aborted before traffic because same-file Compose inheritance did not
  include the original API environment. Fixed by explicitly extending the base
  file; no successful-rollout claim derives from this attempt.
- Run 2: 1,018 requests, no observed errors; blue/green switch, application
  rollback, invalid-port candidate startup, refused switch, restored candidate,
  and business create/process/success checks.
- Run 3: 1,027 requests, no observed errors, including both releases and rollback
  against an added nullable database column. No destructive schema rollback.
- The initial source scan could not analyze Go 1.27 source with its Go 1.26
  build. Its failed output is retained and is not a clean scan. Rebuilding
  govulncheck v1.9.0 using Go 1.27.1 found 11 reachable advisory groups plus
  findings in x/net. Static reachability is conservative, not an exploit proof.
- The original API image failed the Trivy high/critical gate (exit 1), with four
  findings across x/net and the Go standard library. That real baseline serves
  as the detection control; no deliberately vulnerable service was introduced.
- Updated Go 1.27.1 to 1.27.2 and x/net 0.59.0 to 0.60.0, then rebuilt immutable
  API/worker artifacts. Build/runtime images and scanner are pinned by digest.
  Advisory source: [GO-2026-6617](https://pkg.go.dev/vuln/GO-2026-6617),
  [Go release history](https://go.dev/doc/devel/release#go1.27.2).
- Patched source scan: zero reachable vulnerabilities. The unimported
  `golang.org/x/crypto/openpgp` package has an unfixed module advisory
  GO-2026-5932; verbose output records this, without suppressing it.
- Run 4: 1,027 requests, zero observed errors, patched candidate deployment,
  business processing, rollback and refused failed candidate. Run 5 repeats
  the final digest-pinned artifact: 1,004 requests, zero observed errors. See
  its summary and scanner provenance for final identities.
- Patched and final API/worker image reports include CycloneDX SBOMs, current
  database metadata, exit codes and immutable image IDs. No high/critical
  finding is ignored. This is a point-in-time gate, not a guarantee against
  unknown or lower-severity vulnerabilities.

The traffic client reads versions and items about 20 times/second each and
performs four business processing cycles. All requests, durations and event
boundaries are recorded. It is a short sequential low-load experiment, not
capacity testing, a browser check or proof of production zero downtime. No
TLS/authenticated rollout or worker mixed-protocol change is exercised here.
Rollback to the vulnerable baseline is deliberately local and would need a
separate risk decision in production.

## Validation and remaining scope

Go tests and vet, CI-policy regression scripts, shell/Python syntax and composed
model validation accompany the runtime tests. CI now rejects reachable source
vulnerabilities and high/critical findings in both built images; scanner errors
fail closed and reports are retained. Hosted CI remains to be recorded after
publication. Existing correctness/authentication/integration gates are retained.

Rolling, blue/green and canary alternatives are compared in ADR 0015. Production
release provenance/signatures, enforced deployment approvals, rollback-window
policy, longer representative traffic, registry distribution and actual schema
contract changes remain unproven. The deployment profile is intentionally
separate from security and HA profiles; combined guarantees are not inferred.

A final worker scan invocation failed before initialization because the Python
launcher could not resolve its working directory. Repeating with system Python
completed successfully; the final worker report records exit zero. No clean
result was inferred from that failed invocation.
