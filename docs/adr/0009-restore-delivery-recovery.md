# ADR 0009 — Delivery identity and explicit redrive after snapshot restore

## Status

Proposed. Addresses #130 and #131; preserves ADR 0003's success invariant.
Scope is the local Docker reference workload. No exactly-once guarantee.

## Problem

A PostgreSQL snapshot rewinds sequences and outbox publication state while
RabbitMQ retains its messages. Two independent failures were reproduced by
#118: a stale message can target reused numeric IDs, and a restored accepted
job with a published outbox row can have no remaining message.

## Alternatives

| Option | Advantages | Limits |
| --- | --- | --- |
| Purge or quarantine the broker queue on restore | Separates old delivery state from restored state | Requires coordinated routing, delivery suspension, and complete replay; accidental use of the old queue reintroduces aliasing |
| Advance sequences above their pre-loss high-water marks | Numeric IDs remain distinct | A complete high-water mark may be unavailable after loss; handling multiple producers and sequence caching complicates recovery |
| Random durable delivery identity checked by the worker | Does not depend on sequences or a remembered high-water mark; preserves legitimate duplicate delivery | Requires a message protocol migration and an extra database inspection |
| Infer lost delivery from accepted-job age | Can automate retries | Age cannot distinguish loss from slow work, in-flight delivery, or dependency failure; rejected |
| Explicit offline replay of accepted jobs | Enumerates the exact restored accepted set without age guesses; uses existing idempotency | Replays some messages that may already exist; requires a stopped runtime and operator ownership |

## Decision

Use a random UUIDv4 delivery identity in each version 2 outbox payload. It is
created by PostgreSQL in the acceptance transaction and preserved by backup,
publication, redelivery, and redrive. Every worker verifies it against the
persisted outbox before invoking the processor, recording a failure, or
completing a job. A mismatch is rejected without requeue or business effects;
unavailable identity storage requeues. Numeric job and Work Item membership
checks still apply. No title or business input is trusted from a message.

A new job with reused numeric IDs gets a new UUID. An old message cannot
complete it. A message for the same job present in the restored snapshot
retains its UUID and may legitimately redeliver. Existing terminal-state and
transaction guards still provide business idempotency.

Use `tools/work-items-recovery-local inspect` to enumerate accepted jobs and
whether their outbox was marked published. The report does **not** claim that
every candidate has lost its message. After a known offline snapshot restore,
`redrive-after-restore` resets publication responsibility for **all** accepted
jobs. Thus no timing inference or broker inventory is needed to find the
recoverable set. Some deliveries may be duplicated; ADR 0003 handles those.

The operator stops every API and worker process before applying recovery and
keeps them stopped until the command completes. The command verifies the
connected database name, rejects known runtime database connections, validates
pending state, absence of results, one outbox per job, and message identities,
then locks the application tables and resets accepted jobs' `published_at`
inside one serializable transaction. Failed and succeeded jobs, attempts,
job IDs, results, and delivery IDs are preserved. A JSON report identifies the
candidates and application status; save it with the restore evidence.

Offline operation is a required precondition: checking database sessions once
cannot prevent a new process from starting later, and table locks cannot
invalidate an outbox message already held by a running publisher. Do not use
this command as an online automatic timeout or general retry API. Runtime
roles with names other than the repository defaults require an operator to
verify quiescence independently; the command's default-role check is an
additional guard, not a distributed fencing mechanism.

## Upgrade and rollback

Stop API and workers; apply migration 6; deploy both version 2 binaries; restart.
Migration 6 assigns identities to existing outbox rows and returns accepted
jobs to unpublished state. Old version 1 messages are rejected by new workers.
Do not perform a mixed-version rollout. Migration 6 does not add a table or
change the existing four-table backup scope. A pre-migration data-only backup
must be restored with migration 5 first, then migrated to 6 before runtime
starts: restoring old payloads directly into schema 6 is not an upgrade.

Downgrade requires stopped runtime, migration 6 down, and both version 1
binaries. The down migration removes identities and republishes accepted jobs;
protection from reused IDs is then lost. Keep a backup before migration.

## Evidence and limits

The updated snapshot experiment destroys only its validated isolated
PostgreSQL volume, restores the logical snapshot, deliberately reuses IDs with
a stale broker message queued, and checks that no new work executes from that
message. It then recovers the blocked job using explicit redrive and checks a
legitimate duplicate leaves the completed business graph unchanged. See
[the recovery experiment](../experiments/issue-130-131-restore-recovery.md).

UUID collision is extremely unlikely, not mathematically impossible. The
identity is provenance, not authorization or a secret. PostgreSQL and its
operators remain trusted. This decision does not make external side effects
idempotent, recover post-snapshot lost business data, or provide online restore,
PITR, off-site backups, HA, or exactly-once message delivery.

Sources: [PostgreSQL UUID generation](https://www.postgresql.org/docs/18/functions-uuid.html)
and [RabbitMQ reliability and duplicate delivery](https://www.rabbitmq.com/docs/reliability).
