# ADR 0001 — Durable accepted-work recovery boundary

Status: Accepted. Result-state requirements are defined by
[ADR 0003](0003-work-items-async-success-semantics.md); broker coordination by
[ADR 0009](0009-restore-delivery-recovery.md).

## Decision

PostgreSQL is the durable boundary for accepted processing responsibility.
The API commits a job and outbox row before acknowledging acceptance. Recovery
must preserve the Work Item, job, outbox, result and relevant sequence state.
A Work-Item-only backup cannot reconstruct the responsibility already accepted.

Migrations establish schema, roles and grants before a data-only restore.
Database restore does not restore RabbitMQ; fence writers and coordinate
protocol compatibility and offline redrive before resuming runtime.

## Rationale and alternatives

- Backing up only business objects loses accepted but unfinished work.
- A whole-database logical archive includes administration and migration state
  that this controlled application recovery process establishes separately.
- Physical backup/PITR serves a different recovery objective and does not itself
  reconcile broker state or external effects.
- Reconstructing jobs from items cannot reliably determine accepted responsibility.

## Consequences

Keep the archive boundary aligned with application invariants as schema evolves.
Validate archive scope and perform restores into compatible, empty targets.
Snapshot recovery may lose later writes; it does not establish off-site recovery,
production RPO/RTO or exactly-once delivery.

Use the [recovery runbook](../runbooks/work-items-recovery.md) for procedures.
