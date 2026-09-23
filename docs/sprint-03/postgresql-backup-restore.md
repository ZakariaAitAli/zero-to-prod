# Sprint 03 — PostgreSQL Work Item Backup and Destructive Recovery

## Purpose

Issue #99 asks whether Zero-to-Prod can recover persisted Work Item data after complete loss of the local PostgreSQL data volume using a reproducible backup and restore workflow.

The experiment is classified:

    LOCAL-FIRST

It introduces no AWS infrastructure and does not claim production disaster-recovery readiness.

The capability under test is:

    backup and restore of application data
    L0 -> L4

The intended L4 evidence is a working implementation plus deliberate destructive recovery and failure testing.

This work does not establish L5. Managed backups, physical recovery, WAL/PITR, and other representative recovery architectures have not been implemented and compared.

---

## Problem

Before Issue #99, Work Items persisted across normal API and PostgreSQL restarts because the Docker data volume survived.

That behavior was restart persistence, not backup-based recovery.

Destroying the PostgreSQL data volume removed:

- application rows;
- migration-created schema;
- migration metadata;
- sequence state stored in PostgreSQL.

The recovery workflow therefore needed to distinguish between:

- repository-owned schema reconstruction;
- application-data backup;
- destructive database recreation;
- application-data restore;
- recovery verification.

The backup artifact also needed to survive independently of the PostgreSQL data volume.

---

## Environment

The experiment used:

    PostgreSQL 18.6
    pg_dump 18.6
    pg_restore 18.6
    migration version 2
    local Docker Compose
    no AWS resources

The application database is:

    zero_to_prod

The migration/backup identity is:

    zero_to_prod_migrator

The normal runtime identity remains:

    zero_to_prod_app

Generated backup artifacts are stored under:

    .local/postgres-backups/

That path is outside the PostgreSQL data volume and is excluded from Git.

---

## Alternatives evaluated

### Full PostgreSQL logical dump

A full logical dump was inspected.

It contained more than application data, including:

- schema definitions;
- migration metadata;
- ACLs;
- Work Item table data;
- sequence state.

Using that archive as the recovery authority would allow restore to recreate state that is already owned by repository migrations.

This would blur the existing schema-lifecycle boundary.

### Data-only logical dump with migration-first recovery

A data-only dump scoped to:

    public.work_items

contained exactly:

- `TABLE DATA public work_items`;
- `SEQUENCE SET public work_items_id_seq`.

Schema and privilege reconstruction remain the responsibility of explicit repository migrations.

The backup is responsible only for recoverable Work Item data and its identity-sequence state.

### Filesystem or Docker-volume copying

Copying the PostgreSQL storage volume would be tightly coupled to the local storage layout and PostgreSQL physical storage semantics.

That approach was not implemented because the engineering question is application-data recovery while preserving the repository's explicit migration lifecycle.

Physical base backups, WAL archiving, PITR, replication, provider snapshots, and managed backup services remain outside this issue.

---

## Decision

Issue #99 uses:

    explicit migrations
        +
    data-only PostgreSQL logical backup
        +
    data-only PostgreSQL logical restore

The repository migration history remains the schema and privilege authority.

The backup artifact contains only the selected application data and required sequence state.

The recovery order is therefore deliberately:

    recreate PostgreSQL
        ↓
    apply migrations explicitly
        ↓
    restore Work Item data
        ↓
    verify database and application state

The restore tool does not run migrations.

It refuses recovery when the expected schema does not already exist.

### Why this boundary was selected

The full logical dump proved technically capable of carrying schema and privilege information, but those responsibilities already belong to repository migrations.

The data-only archive preserves a clearer ownership boundary:

    repository migrations
        -> schema + privileges

    backup artifact
        -> recoverable application data + sequence position

    application runtime
        -> normal least-privilege reads/writes

This also makes a restore into a completely fresh database visibly dependent on the same migration path used for normal schema reconstruction.

### Reconsideration conditions

This decision should be revisited if the system later requires capabilities such as:

- recovery across incompatible schema versions;
- PostgreSQL physical recovery;
- point-in-time recovery;
- WAL-based recovery;
- very large datasets where logical restore time becomes unacceptable;
- provider-managed backup semantics;
- production retention or off-site storage requirements;
- recovery of state not reproducible from repository migrations.

---

## Repository-owned tooling

Issue #99 adds:

    tools/postgres-backup-local

Supported commands are:

    ./tools/postgres-backup-local create <backup-path>
    ./tools/postgres-backup-local inspect <backup-path>
    ./tools/postgres-backup-local validate <backup-path>
    ./tools/postgres-backup-local restore <backup-path>

Backup creation uses:

    pg_dump
    --format=custom
    --data-only
    --no-owner
    --no-privileges
    --table=public.work_items

The tool writes to a temporary path first and only moves the completed archive to the requested backup path after `pg_dump` succeeds.

It refuses to overwrite an existing backup.

Archive validation requires exactly two archive entries:

    TABLE DATA public work_items
    SEQUENCE SET public work_items_id_seq

A full logical dump therefore fails validation.

Restore uses:

    pg_restore
    --exit-on-error
    --single-transaction

Before restore it verifies that:

- the backup exists;
- the backup is non-empty;
- the archive is readable;
- the archive contains only the expected Work Item data;
- `public.work_items` already exists;
- `public.work_items` is empty.

Restore does not create schema and does not run migrations.

---

## Successful destructive recovery experiment

### Initial data

Before backup, the database contained:

    id=1  before-backup-alpha
    id=2  before-backup-beta

A custom-format data-only backup was created.

After that backup boundary, an additional row was created:

    id=3  after-backup-gamma

The PostgreSQL container, network, and data volume were then destroyed.

The backup artifact remained outside the destroyed database volume and retained the same checksum.

### Recovery

A fresh PostgreSQL environment was created from repository configuration.

Migrations were then applied explicitly.

Migration state after recreation and recovery remained:

    version=2

The retained backup was restored using:

    tools/postgres-backup-local restore

The recovered rows were exactly:

    id=1  before-backup-alpha
    id=2  before-backup-beta

The later row was absent:

    id=3  after-backup-gamma

The same recovered Work Items were verified through the demo API.

This demonstrates successful recovery after destructive data-volume loss without using the original PostgreSQL volume.

---

## Backup boundary and effective local RPO

The experiment deliberately created data on both sides of the backup boundary.

Before backup:

    id=1  before-backup-alpha
    id=2  before-backup-beta

After backup:

    id=3  after-backup-gamma

After destructive loss and restore, only IDs 1 and 2 were recovered.

For this manual snapshot model, the effective recovery point is therefore the moment the backup was created.

Changes made after the backup are outside that recovery point and are lost after destructive recovery.

This is a local demonstration of an RPO boundary.

It is not a production RPO guarantee and does not provide continuous or point-in-time recovery.

---

## Sequence continuity

The data-only archive includes:

    SEQUENCE SET public work_items_id_seq

After restoring IDs 1 and 2, the next API-created Work Item received:

    id=3

The observed sequence state after that insert was:

    last_value=3
    is_called=true

The restore therefore recovered both table rows and the sequence position needed for normal subsequent inserts.

---

## Failure experiments

### Missing backup

A restore was attempted with a missing backup path.

Observed result:

    non-zero exit

The tool reported that the backup file did not exist and did not report successful recovery.

### Corrupt or truncated backup

A corrupt/truncated archive was supplied.

Observed result:

    non-zero exit

Archive validation rejected it as unreadable.

No successful recovery state was reported.

### Full logical dump supplied to data-only restore path

A full logical dump was supplied to `validate`.

Observed result:

    non-zero exit

The archive was rejected because its contents exceeded the exact Work Item data scope required by the recovery contract.

### Restore into non-empty target

Restore was attempted while `public.work_items` already contained data.

Observed result:

    refused before pg_restore

Existing rows remained unchanged.

The workflow therefore does not silently duplicate or overwrite Work Item data.

### Restore before migrations

PostgreSQL was destroyed and recreated without applying migrations.

Restore was then attempted.

Observed error:

    error: restore target schema is not ready; apply migrations explicitly first

`public.work_items` remained absent.

The restore path did not create the schema.

After migrations were applied explicitly, restore succeeded.

This verifies the intended migration-first boundary.

---

## Transactional restore behavior

Restore uses:

    --exit-on-error
    --single-transaction

The deliberately failed restore attempts did not leave partial recovered data behind.

For the tested Work Item archive, recovery either succeeds as one restore transaction or fails without reporting a partial success.

---

## Runtime privilege verification after recovery

Recovery uses:

    zero_to_prod_migrator

It does not broaden the privileges of:

    zero_to_prod_app

After destructive recovery, the runtime role still had the tested privilege boundary:

    schema CREATE: false
    INSERT title: true
    UPDATE: false
    DELETE: false

The API continued to operate through `zero_to_prod_app`.

Backup and restore administration therefore remain separated from normal application runtime access.

---

## Measurements

The measured local backup creation time was:

    0.246 seconds

Backup artifact size:

    1408 bytes

The measured restore command time was:

    0.520 seconds

The measured full destructive recovery path:

    destroy
        -> recreate PostgreSQL
        -> wait for health
        -> apply migrations
        -> restore

was:

    8.514 seconds

These measurements describe one small local dataset on one development machine.

They are evidence about the tested workflow, not production RTO guarantees.

---

## Integrity and safety properties exercised

The experiment verified that:

- backup artifacts are outside the active PostgreSQL data volume;
- generated backup artifacts are excluded from Git;
- an existing backup is not silently overwritten;
- a failed backup does not leave the requested final artifact as if it succeeded;
- archives are validated before target data is modified;
- restore requires explicit schema reconstruction first;
- restore refuses a non-empty Work Item target;
- restore is transactional for the tested archive;
- runtime application privileges remain least-privilege after recovery;
- backup and restore responsibilities remain outside API startup;
- no database password is intentionally encoded in backup filenames;
- no AWS infrastructure is required.

Expected additional AWS infrastructure cost:

    $0

---

## What this experiment proves

For the tested local PostgreSQL environment, Zero-to-Prod can:

- create a repository-owned logical backup of Work Item application data;
- retain that backup independently of the active PostgreSQL volume;
- destroy the database volume completely;
- recreate the database foundation from repository configuration;
- apply migrations explicitly;
- restore the retained Work Item data;
- preserve Work Item sequence continuity;
- verify recovered data directly and through the API;
- reject missing, corrupt, wrong-scope, pre-migration, and non-empty-target restore attempts;
- preserve the existing runtime privilege boundary;
- observe and explain the manual backup recovery-point boundary;
- measure local backup and recovery duration.

This provides the implementation and deliberate failure evidence required for L4 for the specific capability:

    backup and restore of application data

---

## Remaining unknowns

Issue #99 does not prove:

- point-in-time recovery;
- WAL archiving or replay;
- physical backup recovery;
- scheduled backup reliability;
- retention-policy correctness;
- encrypted or off-site backup architecture;
- restore across PostgreSQL major versions;
- recovery of a large dataset;
- recovery under sustained write traffic;
- consistency across multiple databases or external systems;
- high availability or failover;
- provider-managed PostgreSQL recovery;
- RDS or Aurora backup behavior;
- cross-region recovery;
- production RPO or RTO;
- production disaster-recovery readiness.

No L5 claim is made because representative recovery architectures have not been implemented and compared under meaningful operating constraints.

---

## Result

The selected recovery model is:

    repository migrations
        -> reconstruct schema and privileges

    data-only logical backup
        -> preserve Work Item rows and sequence state

    explicit restore
        -> recover into an already-migrated empty target

    direct database + API verification
        -> prove recovered application state

The destructive and failure experiments support moving the specific backup/restore capability from L0 to L4 once the implementation, operational guide/runbook, and repository validation are complete.
