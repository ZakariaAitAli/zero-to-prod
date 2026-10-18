# ADR 0015: immutable blue/green releases in the local delivery lab

Status: accepted for the local lab; see issue-125 experiment evidence. Refs #125.

Use two application slots behind a stable gateway, with the same database and
broker. Build each release once, record its source and Docker image ID, and run
that exact ID. Readiness must succeed before changing the gateway. Validate the
new gateway configuration before reload; keep the old slot for rollback.

Rolling deployment uses less spare capacity but mixes releases and complicates
rollback while instances change. Blue/green needs duplicate application capacity
but makes the traffic switch and rollback explicit for this small workload.
Canary delivery limits initial exposure but needs release-specific business
metrics and enough traffic to distinguish regressions. The local experiment
selects blue/green; it does not establish that it is best at production scale.

Both slots share schema and asynchronous messages. Only additive, backward
compatible schema changes may precede a switch. Destructive contraction waits
until old binaries, queued messages and the rollback window are retired. Database
rollback is not application rollback. Test old and new releases against the same
expanded schema before declaring compatibility.

Measure user-visible request failures during active traffic, including failed
candidate startup and rollback. A readiness probe cannot establish semantic
correctness, so retain business-operation and result checks. Any zero-downtime
claim is limited to the actual measured interval and workload.

Supply-chain checks must include an SBOM, versioned scanner and vulnerability DB
provenance, a deliberately vulnerable negative control, and a patched rebuild.
Scanner errors must fail the gate. Digest pinning prevents silent replacement;
it does not prove provenance or absence of vulnerabilities. Local Docker image
IDs are immutable local artifacts, not signed registry releases. CI gates and
production release signing remain separate requirements to assess explicitly.
