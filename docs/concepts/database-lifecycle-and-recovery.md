# Database lifecycle and recovery

A stateful application introduces responsibilities that do not exist when the runtime can simply be rebuilt from source and configuration.

Zero-to-Prod's Work Items system exercises several of them together:

- durable application data;
- schema migrations;
- runtime privilege separation;
- application/schema compatibility;
- failed migration recovery;
- backup;
- destructive restore;
- recovery verification.

## Schema is deployed state

Application code and database schema evolve on different lifecycles.

The project therefore keeps schema mutation explicit rather than allowing normal application startup to silently migrate the database.

The local lifecycle is:

```text
start PostgreSQL
      ↓
verify database health
      ↓
run migrations explicitly
      ↓
verify schema/migration state
      ↓
start application with runtime identity
```

This makes schema change an observable operational action.

## Migration identity and runtime identity

The application does not need authority to create or alter its schema during normal operation.

The local PostgreSQL lab separates:

- bootstrap administration;
- schema migration;
- application runtime access.

The migration identity owns schema changes. The runtime application identity receives only the table, column, and sequence privileges required by the application contract.

This limits the effect of a compromised or defective application process and makes responsibility clearer.

## Additive evolution creates a compatibility window

Sprint 03 evolved the original Work Items table by adding a non-null `status` column with a compatible default and constrained values.

The experiment explicitly tested:

- old application with the original schema;
- old application with the newer additive schema;
- new application with the newer schema;
- application rollback while retaining the newer schema;
- new application against insufficient old schema.

The safe ordering depended on compatibility, not simply on migration version numbers.

The broader lesson is:

> Application rollback is only safe across a schema change when the retained schema remains compatible with the old application contract.

## Dirty migration state needs investigation

A migration tool can record a dirty version after a failed migration.

The correct response is not automatically "force it back."

The physical database must first be inspected to determine what schema actually exists.

In the tested failure, PostgreSQL rolled back the failed DDL, leaving the physical schema at the previous valid version. Only after verifying that fact was migration metadata reconciled with the known physical state.

A force operation changes migration metadata. It does not repair an unknown schema.

## Backup is not recovery

A backup artifact is useful only if it can participate in a working recovery procedure.

Sprint 03 therefore tested destructive loss rather than stopping at backup creation.

The selected local model was:

```text
repository bootstrap
      ↓
recreate PostgreSQL foundation
      ↓
apply repository migrations
      ↓
restore data-only backup
      ↓
verify database state
      ↓
verify through application behavior
```

Schema and privileges remain defined by repository migrations. The backup contains the scoped application data and sequence state.

## The recovery boundary follows durable state

A backup scope is correct only relative to what the application treats as durable.

The first Work Items backup (Issue #99) covered only `work_items`. That matched the system at the time.

Asynchronous processing then changed the acceptance contract. The API returns `202 Accepted` only after it has committed a `processing_jobs` row and an `outbox_messages` row in the same PostgreSQL transaction. From that point, those rows *are* the accepted responsibility.

As recorded in ADR 0001, restoring the older Work-Item-only backup recovered the business object but lost the accepted asynchronous work. The [Issue #111 experiment](../experiments/issue-111-recovery-model.md) then showed that restoring all three tables let the outbox publish the recovered message and the worker complete the job, without the client resubmitting.

[ADR 0001](../adr/0001-work-items-async-recovery-boundary.md) therefore set the current logical recovery boundary to:

```text
work_items
processing_jobs
outbox_messages
+ their identity sequences
```

ADR 0003 subsequently adds durable title-analysis results to the recovery scope;
the current contract is in the [result recovery runbook](../runbooks/work-items-recovery.md).

The portable lesson:

> The recovery boundary must follow the application's durable state. When a feature adds new state that the system has promised to honour, the backup scope must be revisited, or recovery silently drops that promise.

A backup that still restores cleanly can be the wrong backup.

## Datastore recovery is not distributed recovery

Restoring PostgreSQL recovers what PostgreSQL held. It does not recover state held by other components.

In Work Items, RabbitMQ holds messages after the outbox records them as published. Consider a backup taken in this state:

```text
processing_jobs.state      = accepted
outbox_messages.published_at IS NOT NULL
message in RabbitMQ         = later lost
```

After restore, the job looks accepted and already published. The outbox publisher will not republish it, and the worker will never receive it.

The PostgreSQL backup is correct. The missing piece is a recovery or reconciliation design that spans the database and the broker. Zero-to-Prod has not yet built one.

The general point: in a system with more than one durable component, each component's recovery must be designed, and so must the consistency between them after recovery.

## Recovery point

The tested backup is a manual logical snapshot.

Data committed before the backup boundary can be present in the artifact. Data committed afterward is not.

Sprint 03 demonstrated this by creating data on both sides of the backup boundary and then destroying the active database.

That is an effective recovery-point property of the experiment. It is not point-in-time recovery and is not a production RPO guarantee.

## Restore safety

The restore path deliberately rejects several unsafe or invalid situations, including:

- missing backup;
- corrupt or truncated archive;
- archive with the wrong scope (including an older archive that predates the current boundary);
- restore before every required table exists;
- restore into a target where any required table already has rows.

The tested restore also uses PostgreSQL transactional restore behavior for the scoped archive.

Recovery tooling should prefer a clear failure over silently producing ambiguous state.

Validation is layered and each layer proves less than recovery:

```text
TOC/scope check            — the archive lists exactly the expected entries
payload readability        — every entry's data can be decoded
restore + verification     — the state actually comes back and the application works
```

Only the last establishes recovery.

## Recovery verification

Successful command exit is not the complete recovery claim.

The project verifies recovery at multiple layers:

```text
migration state
      +
database rows
      +
sequence continuity
      +
runtime privilege separation
      +
application-visible data
```

This is analogous to deployment verification: control-plane or tooling success alone is weaker than checking the behavior the system is expected to provide.

## Current boundary

The local experiment does not establish production PostgreSQL recovery.

It does not implement or prove WAL archiving, point-in-time recovery, replication, high availability, managed-database failover, scheduled retention, off-site backup, production-scale restore time, arbitrary cross-version restore, or broker/outbox reconciliation after recovery.

Those remain different capabilities.

## What to remember

- Database schema is deployed state with its own lifecycle.
- Schema mutation does not need to belong to normal application startup.
- Migration and runtime identities have different responsibilities.
- Additive migrations can create useful old/new application compatibility windows.
- Application rollback and schema rollback are different decisions.
- Dirty migration metadata must be reconciled with the real physical schema.
- Creating a backup does not prove recovery.
- A snapshot has a recovery point.
- The backup scope must follow the application's durable state and be revisited when that state changes.
- Recovering one datastore is not recovering the whole distributed system.
- Restore should fail safely when its prerequisites are not satisfied.
- Recovery should be verified through both datastore state and application behavior.

## Zero-to-Prod deep dives

- [Local PostgreSQL and schema migrations](../guides/local-postgresql.md)
- [ADR 0001 — Work Items async recovery boundary](../adr/0001-work-items-async-recovery-boundary.md)
- [Issue #111 — accepted async work recovery experiment](../experiments/issue-111-recovery-model.md)
- [PostgreSQL backup and destructive recovery (Issue #99, Work-Item-only)](../sprint-03/postgresql-backup-restore.md)
- [PostgreSQL schema evolution compatibility](../sprint-03/postgresql-schema-evolution.md)
- [PostgreSQL recovery runbook](../sprint-03/runbook.md)
