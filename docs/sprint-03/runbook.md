# Sprint 03 — PostgreSQL Backup and Recovery Runbook

## Purpose

This runbook covers the tested local recovery path for Work Item data after destructive PostgreSQL data-volume loss.

It is intentionally limited to the Issue #99 LOCAL-FIRST workflow.

It does not cover:

- RDS or Aurora;
- point-in-time recovery;
- WAL archiving;
- replication or failover;
- production retention;
- scheduled backups;
- cross-region recovery.

Expected additional AWS infrastructure cost:

    $0

## Create a backup

PostgreSQL must be running and the Work Item schema must already exist.

From the repository root:

    ./tools/postgres-backup-local create \
      .local/postgres-backups/work-items.dump

The command refuses to overwrite an existing backup.

The resulting artifact is outside the PostgreSQL data volume and is ignored by Git.

## Inspect and validate the backup

Inspect archive contents:

    ./tools/postgres-backup-local inspect \
      .local/postgres-backups/work-items.dump

Validate the recovery contract:

    ./tools/postgres-backup-local validate \
      .local/postgres-backups/work-items.dump

A valid Issue #99 archive contains exactly:

    TABLE DATA public work_items
    SEQUENCE SET public work_items_id_seq

Do not continue with destructive recovery if validation fails.

## Destructive recovery

Destroy the active PostgreSQL container, network, and data volume:

    ./tools/postgres-local destroy

Recreate PostgreSQL:

    ./tools/postgres-local start

Ensure the repository migration image exists:

    ./tools/postgres-local build-migrate

Apply migrations explicitly:

    ./tools/postgres-local migrate-up

Verify migration state:

    ./tools/postgres-local migrate-version

For the current migration set, the expected version is:

    3

Issue #101 expanded the Work Item schema with `status`.

Recovery remains migration-first: reconstruct the current schema before restoring current-version Work Item data.

Restore Work Item data:

    ./tools/postgres-backup-local restore \
      .local/postgres-backups/work-items.dump

The restore command does not run migrations.

If the schema is missing, restore fails with:

    error: restore target schema is not ready; apply migrations explicitly first

If `public.work_items` already contains data, restore is refused.

## Verify recovered database state

Inspect recovered rows directly:

    docker compose \
      -f infra/local/compose.yaml \
      exec -T postgres \
      psql \
        -U zero_to_prod_admin \
        -d zero_to_prod \
        -c 'SELECT id, title, status, created_at FROM public.work_items ORDER BY id;'

Verify migration state again:

    ./tools/postgres-local migrate-version

For the Issue #99 destructive recovery experiment, the recovered rows were:

    id=1  before-backup-alpha
    id=2  before-backup-beta

The post-backup row was intentionally absent:

    id=3  after-backup-gamma

That absence demonstrates the recovery-point boundary of the retained backup.

## Verify through the API

Use the normal local API workflow after PostgreSQL recovery.

Start the API if needed:

    ./tools/demo-api-local run

Verify the application:

    ./tools/demo-api-local verify

Confirm:

    GET /items

returns the same recovered Work Items observed directly in PostgreSQL.

Recovery is not considered demonstrated solely because `pg_restore` exited successfully.

Both datastore and application evidence are required.

## Verify runtime privilege separation

The API must continue using:

    zero_to_prod_app

The tested post-recovery privilege boundary is:

    schema CREATE: false
    INSERT title/status: true
    UPDATE: false
    DELETE: false

Backup and restore use:

    zero_to_prod_migrator

Do not broaden the application runtime role to perform recovery.

## Failure handling

### Missing backup

A missing backup path must fail non-zero.

Do not proceed to destructive recovery if the required artifact is absent.

### Invalid or corrupt archive

Run:

    ./tools/postgres-backup-local validate <backup-path>

before destructive recovery.

Unreadable, truncated, or wrong-scope archives must fail validation.

### Schema not ready

If restore reports:

    error: restore target schema is not ready; apply migrations explicitly first

apply migrations explicitly and verify their version before retrying.

Do not modify the restore tool to run migrations implicitly.

### Non-empty restore target

If restore reports that `public.work_items` is not empty, stop.

The tested workflow deliberately refuses restore into a non-empty target.

Do not manually bypass this guard unless a separate recovery procedure has been designed and tested.

## Backup boundary

This is a manual snapshot model.

The retained backup contains only state captured when the backup was created.

Data committed afterward is not recoverable from that artifact.

For Issue #99:

    backup contains:
      id=1
      id=2

    created after backup:
      id=3

    destructive restore recovered:
      id=1
      id=2

This is the tested local RPO boundary.

It is not point-in-time recovery.

## Measured local timings

Issue #99 observed:

    backup creation:            0.246 seconds
    backup size:                1408 bytes
    restore command:            0.520 seconds
    full destructive recovery:  8.514 seconds

The full recovery measurement covered:

    destroy
      -> recreate PostgreSQL
      -> health
      -> migrations
      -> restore

These are local experiment measurements, not production RTO commitments.

## Clean up backup artifacts

When a retained local backup is no longer required:

    rm -f .local/postgres-backups/work-items.dump

Backup cleanup is separate from PostgreSQL cleanup.

Removing the backup does not modify the active database.

## Recovery boundary

This runbook proves only the tested local logical recovery path.

It does not protect against:

- loss of both the database volume and retained backup;
- corruption not detected before the backup is taken;
- changes after the backup boundary;
- incompatible future schema versions;
- PostgreSQL major-version incompatibility;
- production-scale restore-time requirements;
- region, host, or provider failure;
- missing off-site backup copies.

Detailed Issue #99 experiment evidence and the migration-first/data-only decision are recorded in:

    docs/sprint-03/postgresql-backup-restore.md
