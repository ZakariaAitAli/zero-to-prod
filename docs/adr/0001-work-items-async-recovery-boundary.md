# ADR 0001 — Work Items Async Recovery Boundary

## Status

Accepted

## Context

The Work Items API returns `202 Accepted` only after the asynchronous
processing request has been durably accepted in PostgreSQL.

The durable state consists of:

- `work_items`
- `processing_jobs`
- `outbox_messages`

The original PostgreSQL backup capability was created before asynchronous
processing existed and backed up only `work_items`.

A destructive recovery experiment demonstrated that restoring that older
backup could recover the business object while losing the accepted
asynchronous responsibility.

## Decision

The current Work Items logical recovery boundary includes:

- `work_items`
- `processing_jobs`
- `outbox_messages`

The recovery process remains migration-first:

1. recreate PostgreSQL;
2. apply migrations;
3. restore application data into an empty schema.

The current recovery mechanism remains a selective data-only logical backup
rather than introducing PITR/WAL recovery at this stage.

## Rationale

The selected boundary follows the current acceptance contract.

Once `202 Accepted` has been returned, the system has accepted responsibility
for the asynchronous operation. Recovering only `work_items` would not
preserve enough state to resume that responsibility.

The destructive experiment demonstrated that restoring the three related
tables is sufficient for the current implementation to resume publication
and worker processing without client resubmission.

## Alternatives considered

### Work Item-only backup

Rejected for the current contract because it loses the processing job and
outbox state required to resume accepted asynchronous work.

### Whole-database logical backup

Not selected for the current local-first capability because the requirement
is specifically to recover application state and the existing selective
backup model is smaller and easier to reason about.

This can be reconsidered if the application state or recovery requirements
expand.

### Physical backup / PITR / WAL-based recovery

Not selected at this stage.

These approaches may provide stronger recovery-point characteristics, but they
introduce additional operational complexity that is not currently justified
by a defined production RPO/RTO requirement in this learning-stage system.

They remain candidates for a future recovery-architecture comparison.

### Reconstruct processing state after restore

Not selected because accepted processing state is already durable application
state. Reconstructing it indirectly would create additional recovery logic
and could lose information contained in the original processing/outbox
records.

## Consequences

Positive:

- accepted asynchronous work is included in the recovery boundary;
- the transactional-outbox state can resume after destructive PostgreSQL
  recovery;
- recovery responsibility follows the application's durable state.

Limitations:

- the recovery point still determines what can be recovered;
- no production RPO/RTO is established;
- no PITR/WAL recovery has been implemented or tested;
- RabbitMQ recovery is not addressed by this decision.

## Reconsideration conditions

Reconsider this decision if:

- a production RPO requires recovery points materially tighter than the
  logical backup process can provide;
- PostgreSQL dataset size makes logical recovery operationally unsuitable;
- recovery-time measurements exceed an established RTO;
- production deployment introduces managed PostgreSQL recovery requirements;
- the application changes its acceptance or durability model;
- an architecture comparison demonstrates a materially different recovery
  requirement.

## Evidence

See:

`docs/experiments/issue-111-recovery-model.md`

The experiment included destructive PostgreSQL volume loss, migration-first
reconstruction, restore of accepted async state, RabbitMQ republishing, and
successful worker completion.
