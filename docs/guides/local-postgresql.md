# PostgreSQL and migrations

The local lab runs PostgreSQL in Docker. The API and worker use separate runtime
roles; neither runs schema migrations. The [local setup](local-work-items.md)
starts the complete application.

## Identities

| Identity | Responsibility |
|---|---|
| Bootstrap administrator | Create the database and roles. Keep out of application configuration. |
| Migrator | Apply versioned schema changes and grants. |
| API | Application queries, durable job acceptance and outbox publication. |
| Worker | Processing state and atomic result completion. |

The bootstrap scripts under `infra/local/postgres/` create local development
roles. Migrations under `apps/work-items/migrations/` define application schema
and grants. Do not use the administrator or migrator URL for a runtime process.

## Lifecycle

Run from the repository root:

```sh
./tools/postgres-local start
./tools/postgres-local build-migrate
./tools/postgres-local migrate-up
```

Inspect the target before changing it:

```sh
./tools/postgres-local status
./tools/postgres-local roles
./tools/postgres-local schema
./tools/postgres-local migrate-version
```

Defaults are project `zero-to-prod-local`, database `zero_to_prod` and loopback
port 55432. Set `ZTP_COMPOSE_PROJECT_NAME`, `ZTP_POSTGRES_DB` and
`ZTP_POSTGRES_PORT` consistently when using a different target. The API, worker
and backup tools must use the same target; port overrides do not rewrite their
connection URLs.

## Migration safety

- Apply migrations explicitly. Application startup never changes the schema.
- Use additive, backward-compatible changes while an older release or queued
  message can still be used. Destructive contraction is a separate operation.
- Migration 5 requires empty application tables. Preserve populated legacy
  databases and provision a separate project for the current application.
- A failed empty-table check may leave migration metadata dirty. Inspect schema
  and the precise error before using `migrate-force`; follow the
  [recovery runbook](../runbooks/work-items-recovery.md). Forcing a version is
  not a general repair procedure.
- Migration 6 changes delivery identity and message protocol. Stop runtime
  processes and follow [ADR 0009](../adr/0009-restore-delivery-recovery.md) for
  upgrade, restore and downgrade compatibility. Do not mix protocol versions.

## Logical backups

```sh
./tools/postgres-backup-local create .local/postgres-backups/work-items.dump
./tools/postgres-backup-local inspect .local/postgres-backups/work-items.dump
./tools/postgres-backup-local validate .local/postgres-backups/work-items.dump
```

Creation refuses to overwrite an existing artifact. The archive contains data
for `work_items`, `processing_jobs`, `outbox_messages` and `work_item_results`,
plus sequence state for the three identity-bearing tables. Schema, migration
metadata, roles and privileges are not included; migrations establish those.

Validation checks archive scope and readability. It does not prove a successful
restore. Keep backups outside the database volume, with access controls and a
retention policy. A second directory on the same disk is not off-site recovery.

Use the [recovery runbook](../runbooks/work-items-recovery.md) for restore into
an empty, compatible target and the required checks before resuming runtime.
PostgreSQL restore does not restore RabbitMQ; accepted work may need explicit
offline redrive, and writes after the backup point may be lost.

## Stop

```sh
./tools/postgres-local stop
```

Stop preserves the volume. `destroy` removes it and must be a deliberate,
target-checked action, not a response to an unexplained migration failure.
