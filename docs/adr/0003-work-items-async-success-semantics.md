# ADR 0003 — Work Items Async Success Semantics and Result Ownership

## Status

Proposed

## Context

Work Items have business state `pending | done`; Processing Jobs have execution
state `accepted | succeeded | failed`.

Currently the processor returns `nil` without producing a business result.
The worker then marks the job `succeeded`, while the Work Item remains
`pending`. Clients can also create Work Items as `done`, and multiple jobs
can target the same Work Item.

Issue #116 defines business success before Issue #117 changes implementation.

### Current state machines

The API creates a Work Item as `pending` by default or as client-supplied
`done`. Processing currently makes no Work Item status transition.

Acceptance creates a Processing Job as `accepted` and an outbox message in
one transaction. The existing worker transitions are:

```text
accepted -- processor returns nil; completion update --> succeeded
accepted -- recorded processing failure below limit --> accepted
accepted -- recorded processing failure reaches limit --> failed
```

Completion and recorded failures increment `attempt_count`. Retryable
failures requeue the delivery; the current limit is three recorded processing
failures. Terminal jobs are acknowledged without invoking
the processor again. Database inspection or completion errors requeue delivery
without establishing success. The counter records durable outcomes, not every
invocation or crash. There is no persisted `running` state.

Thus current `succeeded` guarantees a recorded successful processor return,
not a durable business result. Job-state guards protect terminal transitions;
they do not establish idempotency for future business effects by themselves.

## Decision

The Work Item owns one deterministic, PostgreSQL-owned durable business result.
The Processing Job records the execution that attempted to produce it. The
physical schema is deferred to Issue #117.

### Minimal business result

Processing produces a title analysis: the number of Unicode code points and
the number of words in the persisted Work Item title. Words are non-empty
sequences separated by Go's `unicode.IsSpace` whitespace, calculated by the
worker using `strings.Fields`; punctuation does not split words. Analysis
version 1 fixes this whitespace definition; changes to it require a new version.
Counts use the stored title without additional normalization. For example,
`Review API design` produces character count `17` and word count `3`.

The result records the input title, analysis version `1`, and producing job
identity alongside these counts. Identical title input and analysis version
produce identical counts. Job identity is provenance, not part of the
deterministic calculation. This is a small observable application outcome
that exercises result ownership without adding another infrastructure service.

Titles are currently immutable: the API has no update endpoint. Introducing
title edits requires a decision about invalidating results or creating a new
input version before allowing completed items to change.

Work Item business state and job execution state remain separate: several
failed executions may precede the one successful business completion.

Successful processing commits these changes in one PostgreSQL transaction:

1. persist the Work Item result;
2. transition the Work Item from `pending` to `done`;
3. transition the producing Processing Job from `accepted` to `succeeded`.

Both guarded updates must affect exactly one row and verify the job belongs
to the Work Item. Otherwise the entire transaction rolls back, including the
result insertion. Database uniqueness must enforce one result and at most one
accepted or succeeded job per Work Item, with the result referencing its
producing job for that same Work Item. Concurrent workers must serialize or
resolve conflicts by inspecting committed state; a uniqueness conflict alone
is not proof of successful processing.

RabbitMQ acknowledgement follows the commit. PostgreSQL is the source of
truth for business completion; RabbitMQ delivers processing requests.

For each Work Item, the committed success invariant is:

```text
Work Item is done
        ⇕
durable Work Item result exists
        ⇕
the result references the single succeeded job for this Work Item
```

Partial success states are invalid. `done` becomes system-owned: clients
cannot create a Work Item directly as `done`.

Retaining client-created `done` would allow completion without a result;
treating it as an import would require a separate validated result-import
contract. Neither fits this reference system. Issue #117 must reject explicit
`done` with `400 invalid_status`, retain pending/default creation, update the
existing API creation test, and remove `done` creation from the web client.

## Failure and cardinality

A terminal failed job produces no result and leaves the Work Item pending.
Transient retries belong to the same job; a new explicit request after
terminal failure creates a new job.

A Work Item may have multiple jobs over its lifetime, subject to:

- at most one `accepted` job at a time;
- at most one durable result and one successful producing job;
- no new processing job after the Work Item becomes `done`.

These rules must hold for simultaneous processing requests as well as
sequential requests.

### Refused requests and stalled acceptance

Processing requests for an item with an accepted job return `409` with
`processing_already_active`; requests for a done item return `409` with
`work_item_already_done`. Neither creates a job or outbox message. Returning
an existing job instead would require a request-idempotency contract that is
outside this decision. After an uncertain acceptance response, clients inspect
existing item/job state rather than assume no request was accepted. Issue #117
must provide access to the active job identity in the active-conflict response.

An accepted job has no timeout or automatic abandonment under this contract.
Operators restore the publisher, broker, or worker so the existing durable
responsibility can resume. A permanently missing delivery can leave the item
blocked; safe reconciliation or redrive requires separate scoped work. Creating
another job or deleting accepted state is not the recovery mechanism.

### Existing data and migration boundary

Legacy done items, no-op succeeded jobs, and multiple accepted jobs do not
satisfy this invariant. Fabricating results would falsely rewrite their
historical meaning; enforcing the rules only for new rows would weaken the
stated whole-system contract.

For this local learning system, select a fresh empty application database for
the new semantics. Issue #117 must fail its new migration before mutation if
Work Item, job, or outbox data is present, and document provisioning a separate
empty database/volume while retaining the old database and recovery artifacts.
No migration may silently truncate data or reinterpret legacy success.

Historical backups remain recoverable with their matching historical schema
and application. They cannot be restored directly into the new invariant;
post-change backups must include result state. An in-place legacy-data upgrade
requires a separately designed reconciliation/migration path if later needed.

## Storage alternatives

| Model | Consistency and recovery | Decision |
| --- | --- | --- |
| PostgreSQL-only | Result and both transitions share a transaction. Backup and restore must include result data and its job relationship in the same application recovery point. | Selected for small title-analysis data. |
| Local filesystem + PostgreSQL reference | A file write cannot share the PostgreSQL transaction. A crash may leave an orphan file or a database reference to a missing file. Recovery must coordinate database and file backups, preserve stable identities, and reconcile missing/orphaned files. Worker-local files also require shared storage when workers move. | Adds a second durable boundary without a current need. |
| Object storage + PostgreSQL reference | An object upload and database commit are separate writes. Stable object keys and idempotent uploads help retries but do not make the writes atomic. Recovery needs matching object versions and database references, retention rules, and reconciliation for orphaned objects or dangling references. | Reconsider for large artifacts; unnecessary for this result. |

Selection is based on the inspected no-op processor, the small defined result,
and PostgreSQL's ability to own all required state. This is an architectural
comparison, not an experimental proof of filesystem or object-storage behavior.

## Failure ordering

The following is the required failure model, to be tested in Issue #118:

| Ordering | Required outcome |
| --- | --- |
| Crash before result insertion | No business completion; the accepted job may be redelivered. |
| Crash after result insertion or either state update, before commit | All success writes roll back together; Work Item remains pending with no durable result. |
| Crash while commit outcome is uncertain | Inspect durable job/result state on retry before deciding whether to process again. |
| Crash after commit, before ACK | Result, done Work Item, and succeeded job survive together; redelivery recognizes terminal success. |
| Duplicate or concurrent delivery | Computation may repeat, but only one result and successful business transition may commit. |
| Concurrent processing requests | Only one request establishes an accepted job for a pending Work Item. |
| Processing failure and retry exhaustion | Recorded failures remain attempts of the same job; exhaustion marks it failed without committing a result or done state. |

Idempotency concerns committed business effects, not exactly-once execution
or delivery. A failed job may precede a new explicit request. An uncertain
commit must not be converted blindly into a failed job.

The issue's crash-before-RabbitMQ-ACK window after durable success is the
after-commit, before-ACK case above; a pre-commit crash follows rollback rules.

Restoring PostgreSQL must preserve results, Work Items, jobs, and outbox state
from a consistent recovery point. The backup boundary still limits recoverable
work; successful effects after that point may be lost. Separate result storage
would additionally require coordinated recovery and reconciliation as described
above. Existing recovery experiments remain evidence for their original scope.

## Consequences and scope

Issue #117 implements the result representation, constraints, processing
eligibility, and transactional success transition. It must include the result
in the application recovery boundary established by [ADR 0001](0001-work-items-async-recovery-boundary.md).

Issue #118 establishes evidence for crash consistency, duplicate delivery,
idempotency, concurrency, and recovery. This decision alone does not prove
those behaviors.

External effects such as payments, email, third-party API calls, and object
storage writes require a separate consistency decision. Reconsider this
boundary if results become large artifacts or processing requires external
effects.

## Current-state evidence

- `apps/work-items/worker/processor.go`: the default processor returns `nil`.
- `apps/work-items/worker/store.go`: `CompleteProcessingJob` updates job state
  without persisting a result or updating the Work Item.
- `apps/work-items/worker/message.go`: processing, retry, and terminal-job
  settlement behavior.
- `apps/work-items/api/items.go`: the Work Item model permits `done`.
- `apps/work-items/api/store.go`: `AcceptProcessingJob` creates a job and
  outbox message without enforcing the proposed per-item cardinality.
