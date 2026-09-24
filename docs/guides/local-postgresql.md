# Local PostgreSQL and schema migrations

Zero-to-Prod uses a local PostgreSQL environment to verify relational-database lifecycle behavior without AWS infrastructure.

This guide covers the local PostgreSQL lifecycle, explicit schema migrations, runtime privileges, and the demo API persistence dependency.

## Components

The local workflow uses:

- PostgreSQL 18.6
- Docker Compose
- golang-migrate/migrate v4.20.1
- explicit version-controlled SQL migrations

The PostgreSQL image and migration-tool build inputs are pinned for reproducibility.

## Repository layout

    infra/local/compose.yaml
    infra/local/postgres/init/001-roles.sh
    apps/demo-api/migrations/
    tools/migrate/Dockerfile
    tools/postgres-local
    tools/postgres-backup-local

## Responsibility boundaries

Three PostgreSQL roles exist in the local lab.

### Bootstrap administrator

`zero_to_prod_admin`

Created by the PostgreSQL initialization process.

It is a PostgreSQL superuser used only to bootstrap the local database and roles.

Normal schema migrations must not use this identity.

### Migration identity

`zero_to_prod_migrator`

This role:

- can connect to the application database
- has USAGE and CREATE on the public schema
- is not a superuser
- cannot create databases
- cannot create roles
- owns objects created by migrations

Schema migrations run using this identity.

### Application identity

`zero_to_prod_app`

This role:

- can connect to the application database
- has USAGE on the public schema
- cannot create schema objects
- can SELECT the `id`, `title`, `status`, and `created_at` columns from `work_items`
- can INSERT the `title` and `status` columns into `work_items`
- has USAGE on the `work_items` identity sequence
- cannot UPDATE or DELETE `work_items`
- cannot create, alter, or drop application schema objects

The application uses this identity for normal runtime access.

The application must not use the administrator or migration identity for normal runtime access.

## Local credentials

The Compose definition includes predictable development-only credentials so the lab can run without external secret infrastructure.

These credentials are not production secrets and must not be reused outside the local lab.

Environment variables can override the defaults when needed.

## Start PostgreSQL

From the repository root:

    ./tools/postgres-local start

The helper waits until PostgreSQL reports healthy.

PostgreSQL is published only on:

    127.0.0.1:55432

It is not intentionally exposed to the local network.

## Inspect PostgreSQL

Show container state:

    ./tools/postgres-local status

Show database roles:

    ./tools/postgres-local roles

Show the current application schema:

    ./tools/postgres-local schema

## Build the migration tool

Build the repository-owned migration image:

    ./tools/postgres-local build-migrate

The image contains golang-migrate/migrate v4.20.1 with PostgreSQL support.

The migration CLI is operational tooling. It is not linked into the demo API and is not executed automatically when the API starts.

## Apply migrations

Apply all pending migrations explicitly:

    ./tools/postgres-local migrate-up

The initial migration creates Schema A:

    work_items
    ├── id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
    ├── title       TEXT NOT NULL
    └── created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP

Migration 000003 expands that table to Schema AB:

    work_items
    ├── id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
    ├── title       TEXT NOT NULL
    ├── created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
    └── status      TEXT NOT NULL DEFAULT 'pending'

The `status` constraint accepts:

    pending
    done

Migration state is maintained by golang-migrate in the schema_migrations table.

Inspect the current migration version:

    ./tools/postgres-local migrate-version

The current migration set is:

- `000001_create_work_items` — creates Schema A
- `000002_grant_work_items_runtime_privileges` — grants the minimum original runtime privileges required by `zero_to_prod_app`
- `000003_add_work_item_status` — expands Schema A to Schema AB and grants only the additional `status` access required by the evolved application

After all current migrations are applied, the expected version is 3.

Re-running migrate-up when the database is current is expected to make no schema changes.

## Migration lifecycle

Schema mutation is deliberately separate from application startup.

The lifecycle is:

    start PostgreSQL
        ↓
    wait for PostgreSQL health
        ↓
    explicitly run migrations
        ↓
    inspect migration and schema state
        ↓
    start application with zero_to_prod_app

The demo API must not automatically mutate the database schema during normal startup.

If the API starts before the required schema exists, the process can remain alive, but `/ready` returns `503` and persistence operations fail until migrations are applied explicitly.

## Application persistence behavior

The demo API uses PostgreSQL for the current Work Items endpoints:

    POST /items
    GET /items

The API reads and writes through `zero_to_prod_app`.

Readiness uses a bounded, read-only query against the exact columns required by the application. It does not inspect migration-version metadata and does not perform migrations.

The observed behavior is:

- PostgreSQL available with required schema -> `/ready` returns `200`
- PostgreSQL unavailable -> `/health` remains `200`, `/ready` returns `503`
- PostgreSQL available but required schema missing -> `/ready` returns `503`
- persistence operations during database failure -> HTTP `503` with `{"error":"persistence_unavailable"}`
- PostgreSQL recovery -> the same API process can become ready again without restart

## Failure behavior

If PostgreSQL is unavailable, migration commands fail non-zero rather than reporting successful migration state.

Stopping PostgreSQL does not delete the database volume:

    ./tools/postgres-local stop

Starting it again preserves migration and schema state:

    ./tools/postgres-local start

## Dirty migration recovery

Issue #101 deliberately exercised a failed migration that left golang-migrate reporting a dirty version.

A dirty migration must not be repaired by automatically forcing the previous version.

First inspect the physical database and determine which migration version accurately represents the real schema.

Only after that review, reconcile migration metadata explicitly:

    ./tools/postgres-local migrate-force <version>

For the tested Issue #101 failure, PostgreSQL rolled back the failed DDL completely, the physical schema remained at version 3, and the migration metadata was therefore safely reconciled with:

    ./tools/postgres-local migrate-force 3

After forcing metadata, verify both:

    ./tools/postgres-local migrate-version
    ./tools/postgres-local migrate-up

The force command changes migration metadata. It does not inspect or repair partial schema changes automatically.

Detailed evidence is recorded in:

    docs/sprint-03/postgresql-schema-evolution.md

## Destroy and recreate

Destroy the local PostgreSQL container, network, and data volume:

    ./tools/postgres-local destroy

This permanently removes local lab database data.

A fresh environment can then be recreated entirely from repository-defined configuration:

    ./tools/postgres-local start
    ./tools/postgres-local build-migrate
    ./tools/postgres-local migrate-up
    ./tools/postgres-local migrate-version
    ./tools/postgres-local schema

This proves that schema state is reproducible from migrations rather than depending on an existing workstation database.

## Create a Work Item backup

Issue #99 adds a repository-owned logical backup tool for the current Work Item data:

    ./tools/postgres-backup-local create .local/postgres-backups/work-items.dump

The backup uses PostgreSQL custom format and is intentionally data-only.

Its scope is exactly:

    public.work_items table data
    public.work_items_id_seq sequence state

Schema definitions, migration metadata, ownership, and privileges are not part of this backup contract.

Repository migrations remain authoritative for those responsibilities.

The backup path:

    .local/postgres-backups/

is outside the active PostgreSQL data volume and is ignored by Git.

Backup creation refuses to overwrite an existing artifact.

## Inspect and validate a backup

Inspect the PostgreSQL archive:

    ./tools/postgres-backup-local inspect .local/postgres-backups/work-items.dump

Validate that it contains only the expected recovery scope:

    ./tools/postgres-backup-local validate .local/postgres-backups/work-items.dump

Validation requires exactly:

    TABLE DATA public work_items
    SEQUENCE SET public work_items_id_seq

A missing, empty, corrupt, unreadable, or wrong-scope archive fails non-zero.

A full logical database dump is therefore not accepted by this restore workflow.

## Destructive Work Item recovery

Recovery is deliberately migration-first.

Destroying PostgreSQL removes the active local database volume:

    ./tools/postgres-local destroy

Recreate the PostgreSQL foundation:

    ./tools/postgres-local start
    ./tools/postgres-local build-migrate

Apply migrations explicitly:

    ./tools/postgres-local migrate-up
    ./tools/postgres-local migrate-version

Only after the required schema exists, restore the retained application-data backup:

    ./tools/postgres-backup-local restore .local/postgres-backups/work-items.dump

The restore command does not run migrations and does not create schema.

If migrations have not been applied, it fails with:

    error: restore target schema is not ready; apply migrations explicitly first

The restore target must also be empty.

If `public.work_items` already contains rows, restore is refused rather than silently duplicating or overwriting application data.

Restore uses PostgreSQL:

    --exit-on-error
    --single-transaction

for the tested data-only archive.

## Verify recovered state

After recovery, verify migration state:

    ./tools/postgres-local migrate-version

Inspect the restored rows directly in PostgreSQL:

    docker compose \
      -f infra/local/compose.yaml \
      exec -T postgres \
      psql \
        -U zero_to_prod_admin \
        -d zero_to_prod \
        -c 'SELECT id, title, status, created_at FROM public.work_items ORDER BY id;'

Then start or verify the demo API using the normal application workflow and confirm:

    GET /items

returns the same recovered Work Items.

Recovery verification should use both datastore evidence and application behavior.

## Backup boundary

This backup model is a manual logical snapshot.

Rows committed before backup creation are candidates for recovery.

Rows created after that backup boundary are not contained in the existing artifact and are lost if the active database is later destroyed.

The Issue #99 experiment demonstrated this explicitly:

    before backup:
      id=1 before-backup-alpha
      id=2 before-backup-beta

    after backup:
      id=3 after-backup-gamma

After destructive recovery from that backup, only IDs 1 and 2 were restored.

This demonstrates the effective recovery point of the tested snapshot.

It is not point-in-time recovery and is not a production RPO guarantee.

## Recovery responsibility boundary

The local recovery model separates responsibilities:

    repository bootstrap
        -> PostgreSQL roles and database foundation

    repository migrations
        -> schema + runtime privileges

    tools/postgres-backup-local
        -> Work Item data + sequence backup/restore

    zero_to_prod_app
        -> normal application runtime access

The backup and restore workflow uses:

    zero_to_prod_migrator

It does not broaden `zero_to_prod_app` into a migration or administrative role.

The destructive recovery experiment confirmed the runtime role still could not create schema, update rows, or delete rows after restore.

For the full Issue #99 decision, experiment evidence, failure cases, and measurements, see:

    docs/sprint-03/postgresql-backup-restore.md

## Clean up local backup artifacts

Backup files are generated local artifacts and are not intended for Git.

Remove a retained backup explicitly when it is no longer needed:

    rm -f .local/postgres-backups/work-items.dump

Removing the backup does not modify the active PostgreSQL database.

## Cloud cost

This workflow uses local Docker resources only.

No AWS credentials or cloud infrastructure are required.

Expected cloud infrastructure cost: $0.

## Scope boundary

The current local-first implementation now includes:

- persistent Work Items
- `POST /items`
- `GET /items`
- least-privilege runtime database access
- dependency-aware API readiness
- explicit migration lifecycle
- PostgreSQL restart persistence and recovery experiments
- repository-owned Work Item logical backup
- migration-first destructive recovery
- backup validation and restore-target safety checks
- direct database and API-level restored-data verification

It does not yet implement:

- destructive Schema B contraction
- arbitrary cross-version schema compatibility
- cross-version backup restore
- physical PostgreSQL backup
- WAL archiving or point-in-time recovery
- scheduled or production retention policy
- managed PostgreSQL
- replication or high availability
- Redis
- messaging or asynchronous workers
- Kubernetes
