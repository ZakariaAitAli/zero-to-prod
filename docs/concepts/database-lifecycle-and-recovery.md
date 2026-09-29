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

Schema and privileges remain defined by repository migrations. The backup contains the scoped Work Item data and sequence state.

## Recovery point

The tested backup is a manual logical snapshot.

Data committed before the backup boundary can be present in the artifact. Data committed afterward is not.

Sprint 03 demonstrated this by creating data on both sides of the backup boundary and then destroying the active database.

That is an effective recovery-point property of the experiment. It is not point-in-time recovery and is not a production RPO guarantee.

## Restore safety

The restore path deliberately rejects several unsafe or invalid situations, including:

- missing backup;
- corrupt or truncated archive;
- archive with the wrong scope;
- restore before required schema exists;
- restore into a non-empty target.

The tested restore also uses PostgreSQL transactional restore behavior for the scoped archive.

Recovery tooling should prefer a clear failure over silently producing ambiguous state.

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

It does not implement or prove WAL archiving, point-in-time recovery, replication, high availability, managed-database failover, scheduled retention, off-site backup, production-scale restore time, or arbitrary cross-version restore.

Those remain different capabilities.

## What to remember

- Database schema is deployed state with its own lifecycle.
- Schema mutation does not need to belong to normal application startup.
- Migration and runtime identities have different responsibilities.
- Additive migrations can create useful old/new application compatibility windows.
- Application rollback and schema rollback are different decisions.
- Dirty migration metadata must be reconciled with the real physical schema.
- Creating a backup does not prove recovery.
- A snapshot has a recovery boundary.
- Restore should fail safely when its prerequisites are not satisfied.
- Recovery should be verified through both datastore state and application behavior.

## Zero-to-Prod deep dives

- [Local PostgreSQL and schema migrations](../guides/local-postgresql.md)
- [PostgreSQL backup and destructive recovery](../sprint-03/postgresql-backup-restore.md)
- [PostgreSQL schema evolution compatibility](../sprint-03/postgresql-schema-evolution.md)
- [PostgreSQL recovery runbook](../sprint-03/runbook.md)
