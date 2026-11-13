# Issues #130/#131 — restore delivery identity and offline redrive

Date: 2026-10-18. Decision: [ADR 0009](../adr/0009-restore-delivery-recovery.md).
Scope: a local Docker lab, real PostgreSQL and RabbitMQ, actual API and worker
processes. Authentication is disabled in this recovery experiment; it does
not retest the security lab. This is snapshot recovery, not PITR or HA.

## Method

Adapt the #118 E12 experiment, preserving its historical evidence. Build a
snapshot containing completed B, published-but-accepted C, and unpublished A.
After the snapshot, complete A and C, and publish new job D with no consumer.
Stop runtime, destroy only the validated isolated PostgreSQL volume, provision
migration-first, and restore the snapshot. RabbitMQ still holds D's message.
Create E while publication is disabled so that E reuses D's numeric job and
Work Item IDs. Start the worker with only the stale D message queued. Inspect
E's durable graph before allowing its own outbox message to publish.

Then resume normal publication and check A and E complete. C has a published
outbox but no message and a new processing request returns 409. Stop runtime,
inspect the accepted set, explicitly redrive after restore, and start runtime.
Check the same C job completes and a legitimate duplicate changes neither
attempt count nor business state. B remains unchanged.

## Results

| Check | Result |
| --- | --- |
| Snapshot/restore with actual process lifecycle | 42 checks passed, 0 failed |
| Numeric IDs reused by E | Same item and job IDs as D |
| Stale D message | Settled; E stays pending and accepted, attempts 0, no result |
| Normal E publication | E completes once from its own version 2 message |
| C before redrive | Accepted, published outbox, no message; new request returns 409 |
| Offline inspection | Identifies C as the sole accepted candidate, without a timing threshold |
| Explicit redrive | Same job and delivery identity recover; no replacement job |
| Legitimate duplicate | Completed graph unchanged |
| Previously completed B | Unchanged |
| Crash after commit before acknowledgement regression | 8 checks passed |
| Concurrent duplicate delivery regression | 7 checks passed |

[Snapshot evidence](../../evidence/issue-130-131/e12-snapshot-recovery.json),
[crash regression](../../evidence/issue-130-131/regressions/e4-crash-after-commit-before-ack.json),
[concurrent duplicate regression](../../evidence/issue-130-131/regressions/e6-concurrent-duplicate-delivery.json),
and [source hashes and run scope](../../evidence/issue-130-131/provenance.json).

The operator integration test separately refused a wrong database, an idle
runtime connection, and a corrupted restored payload. Repeated offline
redrive preserved job count, attempts, publication attempts, identity, and the
terminal job's publication state. Populated migration 6 down/up changed the
protocol as intended and returned accepted work to publication responsibility.

Default Go tests and vet, API/worker integration tests, result migration and
backup compatibility, and CI workflow policy tests passed. The workflow now
runs the operator integration test with a required environment and refuses
skipped tests. The full #118 crash suite and security lab suite were not rerun.

## Reproduction

Use two isolated Compose projects beginning `zero-to-prod-118`, separate from
retained projects. Configure the existing crash harness's PostgreSQL, RabbitMQ,
fixture, and runtime URL variables, and `ISSUE118_EVIDENCE_DIR` to a new output
directory. Set `ISSUE118_ALLOW_DESTROY` to the exact PostgreSQL project only
when its volume can be destroyed. Run:

```bash
go -C apps/work-items test -tags crashexperiment ./worker \
  -run '^TestIssue130131SnapshotRecovery$' -count=1 -timeout=8m -v
```

The harness validates the project prefix, loopback ports, container/volume
labels, mounts, and PostgreSQL system identity before destruction. It also
recreates its own explicit-subnet network after destruction, avoiding the
exhausted default address pool. Do not point it at retained application data.
The operator procedure is in the [recovery guide](../guides/restore-delivery-recovery.md).

## Limits and cleanup

The test is a single local run. UUID collision is extremely unlikely, not
impossible. An offline command cannot fence a supervisor that restarts runtime
behind the operator's back; quiescence is required. The report enumerates
accepted candidates, not proof of the absence of every individual message.
Duplicate delivery remains expected; no exactly-once claim is made.

The experiment used new projects `zero-to-prod-118-recovery-pg` and
`zero-to-prod-118-recovery-mq`, ports 55440/5678/15678 and explicit subnets
10.213.252.0/24 and 10.213.253.0/24. Only the new PostgreSQL volume was destroyed
and recreated. Retained projects were not started or modified. The two new
projects are stopped with volumes and networks retained for review. Temporary
operator-test databases were dropped by their test cleanup. No configured
credential, connection URL, or private key was found in the JSON evidence.
