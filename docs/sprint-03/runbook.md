# Sprint 03 — PostgreSQL Backup and Recovery Runbook

> This is the retained Issue #99 recovery record. For migration 5 and title-analysis
> results, use the [current recovery runbook](../runbooks/work-items-recovery.md).

## Purpose

This runbook covers the current local recovery path for the persisted Work Items accepted-work state after destructive PostgreSQL data-volume loss.

It is the current PostgreSQL backup and recovery runbook. It lives under `sprint-03/` because the capability was introduced in Sprint 03.

The recovery boundary is:

    work_items
    processing_jobs
    outbox_messages

plus their identity sequences, as decided in [ADR 0001](../adr/0001-work-items-async-recovery-boundary.md).

The workflow was introduced by Issue #99 for `work_items` only. It was extended to the three-table boundary after the [Issue #111 recovery experiment](../experiments/issue-111-recovery-model.md). Sections below that quote Issue #99 rows or timings are historical observations, not the current contract.

It is LOCAL-FIRST.

It does not cover:

- RabbitMQ state backup or recovery;
- reconciliation of outbox rows already marked published whose messages the broker has lost;

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

PostgreSQL must be running and migrations must already be applied (current version: 4).

From the repository root:

    ./tools/postgres-backup-local create \
      .local/postgres-backups/work-items.dump

The command refuses to overwrite an existing backup.

The resulting artifact is outside the PostgreSQL data volume and is ignored by Git.

## Inspect and validate the backup

Inspect archive contents:

    ./tools/postgres-backup-local inspect \
      .local/postgres-backups/work-items.dump

Validate the archive:

    ./tools/postgres-backup-local validate \
      .local/postgres-backups/work-items.dump

A valid current archive contains exactly six TOC entries:

    TABLE DATA public work_items
    TABLE DATA public processing_jobs
    TABLE DATA public outbox_messages
    SEQUENCE SET public work_items_id_seq
    SEQUENCE SET public processing_jobs_id_seq
    SEQUENCE SET public outbox_messages_id_seq

After the scope check passes, `validate` decodes every archive payload with `pg_restore --file=/dev/null`. It does not connect to a database or execute archive SQL.

A successful `validate` establishes only these two things:

    TOC/scope validation          — the archive lists exactly the expected entries
    payload readability           — every entry's data can be decoded

Neither is successful recovery. Recovery is established only by restoring and verifying, as described below.

An older Issue #99 archive (two entries, Work Items only) fails current validation and cannot be restored with the current tool.

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

    4

Recovery remains migration-first: reconstruct the current schema before restoring current-version application data.

Restore the accepted-work state:

    ./tools/postgres-backup-local restore \
      .local/postgres-backups/work-items.dump

Restore re-runs archive validation first, then checks the target, and only then runs `pg_restore` with `--exit-on-error --single-transaction`.

The restore command does not run migrations.

The restore target must satisfy both preconditions:

- `public.work_items`, `public.processing_jobs`, and `public.outbox_messages` all exist;
- all three tables are empty.

If any table is missing, restore fails with:

    error: restore target schema is not ready; apply migrations explicitly first

If any table contains rows, restore is refused with the three row counts:

    error: restore target application data is not empty (<work_items>|<processing_jobs>|<outbox_messages>)

## Verify recovered database state

Inspect recovered rows directly:

    docker compose \
      -f infra/local/compose.yaml \
      exec -T postgres \
      psql \
        -U zero_to_prod_admin \
        -d zero_to_prod \
        -c 'SELECT id, title, status, created_at FROM public.work_items ORDER BY id;' \
        -c 'SELECT id, work_item_id, state, attempt_count FROM public.processing_jobs ORDER BY id;' \
        -c 'SELECT id, processing_job_id, publish_attempts, published_at FROM public.outbox_messages ORDER BY id;'

Verify migration state again:

    ./tools/postgres-local migrate-version

Historical example — for the Issue #99 destructive recovery experiment (Work-Item-only archive), the recovered rows were:

    id=1  before-backup-alpha
    id=2  before-backup-beta

The post-backup row was intentionally absent:

    id=3  after-backup-gamma

That absence demonstrates the recovery-point boundary of the retained backup.

## Verify through the API

Use the normal local API workflow after PostgreSQL recovery.

Start the API if needed:

    ./tools/work-items-api-local run

Verify the application:

    ./tools/work-items-api-local verify

Confirm:

    GET /items
    GET /processing-jobs/{id}

return the same recovered state observed directly in PostgreSQL.

## Verify resumed asynchronous work

If the restore recovered processing jobs in state `accepted`, keep the API running (it hosts the outbox publisher) and start RabbitMQ and the worker:

    ./tools/rabbitmq-local start
    ./tools/work-items-worker-local run

Then confirm:

- outbox rows that were restored with `published_at IS NULL` receive a `published_at` value;
- the corresponding processing jobs reach a terminal state (`succeeded` or `failed`);
- the RabbitMQ queue drains.

The [Issue #111 experiment](../experiments/issue-111-recovery-model.md) observed this sequence for one accepted, unpublished job.

Restored jobs that are `accepted` but whose outbox row already has `published_at` set will **not** be republished. See [Recovery boundary](#recovery-boundary).

Recovery is not considered demonstrated solely because `pg_restore` exited successfully.

Both datastore and application evidence are required.

## Verify runtime privilege separation

The API must continue using:

    zero_to_prod_app

The worker must continue using:

    zero_to_prod_worker

The post-recovery privilege boundary tested in Issue #99 for `zero_to_prod_app` on `work_items` was:

    schema CREATE: false
    INSERT title/status: true
    UPDATE: false
    DELETE: false

Backup and restore use:

    zero_to_prod_migrator

Do not broaden the runtime roles to perform recovery.

## Failure handling

### Missing backup

A missing backup path must fail non-zero.

Do not proceed to destructive recovery if the required artifact is absent.

### Invalid or corrupt archive

Run:

    ./tools/postgres-backup-local validate <backup-path>

before destructive recovery.

Validation fails non-zero for:

- an unreadable TOC: `error: backup archive is not readable`;
- a wrong entry count, for example an older two-entry Issue #99 archive or a full database dump: `error: unexpected backup archive contents (<n> entries)`;
- six entries with the wrong scope: `error: backup archive is missing required entry: ...`;
- a readable TOC whose payload is truncated or corrupt: `error: backup archive payload is not readable`.

Do not attempt restore from an archive that fails validation. If no valid archive exists, the accepted-work state cannot be recovered by this procedure.

### Schema not ready

If restore reports:

    error: restore target schema is not ready; apply migrations explicitly first

apply migrations explicitly and verify their version before retrying.

Do not modify the restore tool to run migrations implicitly.

### Non-empty restore target

If restore reports `restore target application data is not empty`, stop.

The reported counts are for `work_items|processing_jobs|outbox_messages`. Any non-zero count blocks restore.

The workflow deliberately refuses restore into a non-empty target.

Do not manually bypass this guard unless a separate recovery procedure has been designed and tested.

## Backup boundary

This is a manual snapshot model.

The retained backup contains only state captured when the backup was created.

Data committed afterward is not recoverable from that artifact.

Historical example — for Issue #99:

    backup contains:
      id=1
      id=2

    created after backup:
      id=3

    destructive restore recovered:
      id=1
      id=2

This was the tested local RPO boundary for that experiment.

It is not point-in-time recovery.

## Measured local timings

Issue #99 observed (Work-Item-only archive; the current three-table archive has not been re-measured):

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

This runbook recovers only the persisted PostgreSQL accepted-work state.

PostgreSQL recovery is not complete distributed recovery. RabbitMQ state is outside the backup. In particular, if a backup captured:

    processing_jobs.state = accepted
    outbox_messages.published_at IS NOT NULL

and the broker later lost that message, restore brings back a job that the outbox publisher will not republish and that nothing currently reconciles. Recovering that delivery responsibility needs a separate recovery or reconciliation design, which does not exist yet.

The runbook also does not protect against:

- loss of both the database volume and retained backup;
- corruption not detected before the backup is taken;
- changes after the backup boundary;
- incompatible future schema versions;
- PostgreSQL major-version incompatibility;
- production-scale restore-time requirements;
- region, host, or provider failure;
- missing off-site backup copies.

The original Issue #99 experiment evidence and the migration-first/data-only decision are recorded in:

    docs/sprint-03/postgresql-backup-restore.md

The current three-table recovery boundary and its destructive-recovery evidence are recorded in:

    docs/adr/0001-work-items-async-recovery-boundary.md
    docs/experiments/issue-111-recovery-model.md
