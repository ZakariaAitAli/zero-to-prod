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
- can SELECT the `id`, `title`, and `created_at` columns from `work_items`
- can INSERT only the `title` column into `work_items`
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

The initial migration creates:

    work_items
    ├── id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
    ├── title       TEXT NOT NULL
    └── created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP

Migration state is maintained by golang-migrate in the schema_migrations table.

Inspect the current migration version:

    ./tools/postgres-local migrate-version

The current migration set is:

- `000001_create_work_items` — creates the Work Items schema
- `000002_grant_work_items_runtime_privileges` — grants the minimum runtime privileges required by `zero_to_prod_app`

After both migrations are applied, the expected version is 2.

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

A deliberate dirty-migration recovery experiment is outside this foundation issue and belongs to later schema-evolution work.

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

It does not yet implement:

- schema A/AB/B compatibility
- dirty migration recovery
- backup and restore
- managed PostgreSQL
- replication or high availability
- Redis
- messaging or asynchronous workers
- Kubernetes
