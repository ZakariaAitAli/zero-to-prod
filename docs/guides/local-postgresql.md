# Local PostgreSQL and schema migrations

Zero-to-Prod uses a local PostgreSQL environment to verify relational-database lifecycle behavior without AWS infrastructure.

This guide covers the local PostgreSQL lifecycle, explicit schema migrations, runtime privileges, and the Work Items API persistence dependency.

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
    apps/work-items/migrations/
    tools/migrate/Dockerfile
    tools/postgres-local
    tools/postgres-backup-local

## Responsibility boundaries

Four PostgreSQL roles exist in the local lab.

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
- can INSERT only `title` into `work_items`; migration 5 revokes the `status` insertion granted by migration 3, so new items always start `pending`
- cannot UPDATE or DELETE `work_items`
- can SELECT the job columns of `processing_jobs` and INSERT only `work_item_id`
- can SELECT `outbox_messages`, INSERT `processing_job_id`, `event_type`, and `payload`, and UPDATE only the publication columns (`publish_attempts`, `last_error_code`, `published_at`)
- can SELECT `work_item_results` but not write it
- has USAGE on the `work_items`, `processing_jobs`, and `outbox_messages` identity sequences
- cannot create, alter, or drop application schema objects

The API uses this identity for normal runtime access, including the API-embedded outbox publisher.

### Worker identity

`zero_to_prod_worker`

This role:

- can SELECT the `id`, `title`, `status`, and `created_at` columns from `work_items` and UPDATE only `status`
- can SELECT the job columns of `processing_jobs` and UPDATE only the processing-outcome columns (`state`, `attempt_count`, `last_error_code`, `finished_at`)
- can SELECT and INSERT `work_item_results`
- cannot insert Work Items or processing jobs, and has no access to `outbox_messages`
- cannot create, alter, or drop application schema objects

The worker uses this identity for normal runtime access.

The grants are cumulative across migrations: `000002` (original `work_items`
access), `000003` (`status`), `000004` (`processing_jobs`, `outbox_messages`,
and the worker identity), and `000005` (result access, worker `status` update,
and revocation of API `status` insertion). Inspect a running database with
`./tools/postgres-local roles` and `\dp` in `psql`.

The API and worker must not use the administrator or migration identity for normal runtime access.

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

The migration CLI is operational tooling. It is not linked into the Work Items API and is not executed automatically when the API starts.

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

Migration 000004 adds the durable asynchronous-processing state:

    processing_jobs
    ├── id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
    ├── work_item_id     BIGINT NOT NULL -> work_items.id
    ├── state            TEXT NOT NULL DEFAULT 'accepted'  (accepted | succeeded | failed)
    ├── attempt_count    INTEGER NOT NULL DEFAULT 0
    ├── last_error_code  TEXT
    ├── created_at       TIMESTAMPTZ NOT NULL
    └── finished_at      TIMESTAMPTZ

    outbox_messages
    ├── id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
    ├── processing_job_id  BIGINT NOT NULL UNIQUE -> processing_jobs.id
    ├── event_type         TEXT NOT NULL  (work_item.process)
    ├── payload            JSONB NOT NULL
    ├── publish_attempts   INTEGER NOT NULL DEFAULT 0
    ├── last_error_code    TEXT
    ├── created_at         TIMESTAMPTZ NOT NULL
    └── published_at       TIMESTAMPTZ

Migration state is maintained by golang-migrate in the schema_migrations table.

Inspect the current migration version:

    ./tools/postgres-local migrate-version

The current migration set is:

- `000001_create_work_items` — creates Schema A
- `000002_grant_work_items_runtime_privileges` — grants the minimum original runtime privileges required by `zero_to_prod_app`
- `000003_add_work_item_status` — expands Schema A to Schema AB and grants only the additional `status` access required by the evolved application
- `000004_add_async_processing` — creates `processing_jobs` and `outbox_messages` and grants the API and worker identities only the columns they need
- `000005_add_title_analysis` — adds `work_item_results`, completion constraints, and system-owned status privileges

Migration 000005 requires empty application tables; preserve legacy data in its original database. See the [result recovery runbook](../runbooks/work-items-recovery.md), including expected dirty-version recovery.

After all current migrations are applied, the current migration version is 5.

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

The Work Items API must not automatically mutate the database schema during normal startup.

If the API starts before the required schema exists, the process can remain alive, but `/ready` returns `503` and persistence operations fail until migrations are applied explicitly.

## Application persistence behavior

The Work Items API uses PostgreSQL for the current endpoints:

    POST /items
    GET /items
    POST /items/{id}/process
    GET /processing-jobs/{id}

The API reads and writes through `zero_to_prod_app`.

Readiness runs bounded, read-only `LIMIT 0` queries against the exact columns the API requires in all four relations:

    work_items
    processing_jobs
    outbox_messages
    work_item_results

If any relation or required column is missing or inaccessible, `/ready` returns `503`. Readiness does not inspect migration-version metadata and does not perform migrations.

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

For the tested Issue #101 failure (when the current schema version was 3), PostgreSQL rolled back the failed DDL completely, the physical schema remained at version 3, and the migration metadata was therefore safely reconciled with:

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

## Create a backup

`tools/postgres-backup-local` creates a repository-owned logical backup of the persisted accepted-work state:

    ./tools/postgres-backup-local create .local/postgres-backups/work-items.dump

The backup uses PostgreSQL custom format and is intentionally data-only.

Its scope is exactly:

    public.work_items table data
    public.processing_jobs table data
    public.outbox_messages table data
    public.work_item_results table data
    public.work_items_id_seq sequence state
    public.processing_jobs_id_seq sequence state
    public.outbox_messages_id_seq sequence state

`processing_jobs` and `outbox_messages` are included because, once the API has returned `202 Accepted`, they hold the asynchronous responsibility the system has accepted. See [ADR 0001](../adr/0001-work-items-async-recovery-boundary.md).

Schema definitions, migration metadata, ownership, and privileges are not part of this backup contract.

Repository migrations remain authoritative for those responsibilities.

The backup path:

    .local/postgres-backups/

is outside the active PostgreSQL data volume and is ignored by Git.

Backup creation refuses to overwrite an existing artifact.

## Inspect and validate a backup

Inspect the PostgreSQL archive:

    ./tools/postgres-backup-local inspect .local/postgres-backups/work-items.dump

Validate the archive:

    ./tools/postgres-backup-local validate .local/postgres-backups/work-items.dump

Validation performs two checks:

1. **TOC/scope:** the archive table of contents must contain exactly these seven entries:

        TABLE DATA public work_items
        TABLE DATA public processing_jobs
        TABLE DATA public outbox_messages
        TABLE DATA public work_item_results
        SEQUENCE SET public work_items_id_seq
        SEQUENCE SET public processing_jobs_id_seq
        SEQUENCE SET public outbox_messages_id_seq

2. **Payload readability:** every archive payload is decoded with `pg_restore --file=/dev/null`, without connecting to a database or executing archive SQL.

A missing, empty, unreadable, truncated, or wrong-scope archive fails non-zero.

A full logical database dump is not accepted by this restore workflow. Neither is an older Issue #99 Work-Item-only archive (two entries).

Validation is not recovery. A readable, correctly scoped archive can still fail to restore, and successful restore still needs verification:

    TOC/scope validation
      ≠ complete archive payload readability
      ≠ successful recovery

## Destructive recovery

Recovery is deliberately migration-first.

Destroying PostgreSQL removes the active local database volume:

    ./tools/postgres-local destroy

Recreate the PostgreSQL foundation:

    ./tools/postgres-local start
    ./tools/postgres-local build-migrate

Apply migrations explicitly:

    ./tools/postgres-local migrate-up
    ./tools/postgres-local migrate-version

The expected version is 5.

Only after the required schema exists, restore the retained application-data backup:

    ./tools/postgres-backup-local restore .local/postgres-backups/work-items.dump

Restore first re-runs archive validation. It then checks the target before touching it.

The restore command does not run migrations and does not create schema.

All four tables must exist:

    public.work_items
    public.processing_jobs
    public.outbox_messages
    public.work_item_results

If any is missing, restore fails with:

    error: restore target schema is not ready; apply migrations explicitly first

All four tables must also be empty.

If any of them contains rows, restore is refused rather than silently duplicating or overwriting application data:

    error: restore target application data is not empty (<work_items>|<processing_jobs>|<outbox_messages>|<work_item_results>)

Restore uses PostgreSQL:

    --exit-on-error
    --single-transaction

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
        -c 'SELECT id, title, status, created_at FROM public.work_items ORDER BY id;' \
        -c 'SELECT id, work_item_id, state, attempt_count FROM public.processing_jobs ORDER BY id;' \
        -c 'SELECT id, processing_job_id, publish_attempts, published_at FROM public.outbox_messages ORDER BY id;' \
        -c 'SELECT work_item_id, processing_job_id, character_count, word_count FROM public.work_item_results ORDER BY work_item_id;'

Then start or verify the Work Items API using the normal application workflow and confirm:

    GET /items
    GET /processing-jobs/{id}

return the same recovered state.

If unpublished outbox rows were restored, start RabbitMQ, the API, and the worker, then confirm that the outbox publisher records `published_at` and the processing job reaches a terminal state.

Recovery verification should use both datastore evidence and application behavior.

## Backup boundary

This backup model is a manual logical snapshot.

Rows committed before backup creation are candidates for recovery.

Rows created after that backup boundary are not contained in the existing artifact and are lost if the active database is later destroyed.

The historical Issue #99 experiment (Work-Item-only archive) demonstrated this explicitly:

    before backup:
      id=1 before-backup-alpha
      id=2 before-backup-beta

    after backup:
      id=3 after-backup-gamma

After destructive recovery from that backup, only IDs 1 and 2 were restored.

This demonstrates the effective recovery point of the tested snapshot.

It is not point-in-time recovery and is not a production RPO guarantee.

The [Issue #111 experiment](../experiments/issue-111-recovery-model.md) then showed that a three-table archive taken while a processing job was `accepted` and its outbox message unpublished could be restored after destructive loss, and that the job was published and completed without client resubmission.

## PostgreSQL recovery is not broker recovery

The backup protects persisted accepted-work state. It does not capture RabbitMQ state.

If an outbox message was already marked `published_at` before the backup and the broker later loses that message, restore brings back:

    processing_jobs.state = accepted
    outbox_messages.published_at IS NOT NULL

The outbox publisher will not republish it, and nothing currently reconciles that state.

Recovering broker-held delivery responsibility needs a separate recovery or reconciliation design. This backup does not provide one. The crash-consistency experiment reproduced this blocked state and a second divergence, stale queued messages targeting reused IDs; both remain open as [#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130) and [#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131). See the [current recovery runbook](../runbooks/work-items-recovery.md).

## Recovery responsibility boundary

The local recovery model separates responsibilities:

    repository bootstrap
        -> PostgreSQL roles and database foundation

    repository migrations
        -> schema + runtime privileges

    tools/postgres-backup-local
        -> accepted-work and result data + sequence backup/restore
           (work_items, processing_jobs, outbox_messages, work_item_results)

    zero_to_prod_app / zero_to_prod_worker
        -> normal runtime access

The backup and restore workflow uses:

    zero_to_prod_migrator

It does not broaden the runtime identities into migration or administrative roles.

The historical Issue #99 destructive recovery experiment confirmed that the runtime role still could not create schema, update rows, or delete rows after restore.

For the original Issue #99 decision, experiment evidence, failure cases, and measurements, see:

    docs/sprint-03/postgresql-backup-restore.md

For the historical three-table boundary, see:

    docs/adr/0001-work-items-async-recovery-boundary.md
    docs/experiments/issue-111-recovery-model.md

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
- durable asynchronous-processing state (`processing_jobs`, `outbox_messages`)
- separate least-privilege API and worker runtime database access
- dependency-aware API readiness across all four relations
- explicit migration lifecycle
- PostgreSQL restart persistence and recovery experiments
- repository-owned logical backup of the accepted-work state
- migration-first destructive recovery
- backup scope and payload-readability validation and restore-target safety checks
- direct database and API-level restored-data verification

RabbitMQ messaging and the asynchronous worker are implemented; see the [local Work Items guide](local-work-items.md).

It does not yet implement:

- destructive Schema B contraction
- arbitrary cross-version schema compatibility
- cross-version backup restore
- physical PostgreSQL backup
- WAL archiving or point-in-time recovery
- scheduled or production retention policy
- managed PostgreSQL
- replication or high availability
- RabbitMQ state backup or broker/outbox reconciliation after recovery
- Redis
- Kubernetes
