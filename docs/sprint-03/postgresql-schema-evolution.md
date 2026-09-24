# Sprint 03 — PostgreSQL Schema Evolution Compatibility

## Problem

Zero-to-Prod already had:

- explicit PostgreSQL migrations;
- a persistent Work Item API;
- dependency-aware readiness;
- least-privilege runtime database access;
- destructive backup and restore.

The next unresolved problem was schema evolution.

A database schema can change before, during, or after an application rollout. If the application and schema are not deliberately compatible during that transition, a deployment can become impossible to roll forward or roll back safely.

Issue #101 tests a narrow local-first question:

> Can the Work Item schema evolve additively while preserving old-application compatibility, failing safely when a new application sees an insufficient schema, and recovering explicitly from migration failure?

Expected additional AWS infrastructure cost:

    $0

No AWS resources are required by this experiment.

## Starting state — Schema A

Before Issue #101, `public.work_items` contained:

    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
    title       TEXT NOT NULL
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP

The existing application understood exactly those columns.

The migration version was:

    2

## Target compatibility state — Schema AB

Issue #101 introduces migration:

    000003_add_work_item_status

It expands `public.work_items` with:

    status TEXT NOT NULL DEFAULT 'pending'

and the constraint:

    status IN ('pending', 'done')

The additive state is called Schema AB because it supports both:

- the old application contract, which does not know about `status`;
- the new application contract, which requires `status`.

The migration also grants the runtime application role only:

    SELECT(status)
    INSERT(status)

It does not grant UPDATE, DELETE, schema CREATE, or schema ownership.

## Why the migration is additive

The previous application inserts only:

    title

It does not provide a value for `status`.

The database default:

    'pending'

allows those old inserts to remain valid after migration 000003.

The old application also uses explicit column lists instead of `SELECT *`, so the existence of the additional column does not change its scan contract.

This creates a deliberate compatibility window rather than requiring the database and application to change atomically.

## New application contract

The evolved API accepts Work Item status values:

    pending
    done

If `status` is omitted from `POST /items`, the application defaults it to:

    pending

Unknown status values fail with:

    HTTP 400
    {"error":"invalid_status"}

The database also enforces the allowed values with a CHECK constraint.

This provides validation at both:

- the HTTP/application boundary;
- the persistence boundary.

## Readiness contract

The evolved application requires:

    id
    title
    status
    created_at

Its readiness query therefore references those exact columns.

The application does not use migration-version metadata as a readiness signal.

This distinction matters:

    /health
        -> process liveness

    /ready
        -> application can safely serve traffic with its required persistence contract

A process may therefore remain alive while being unready because the required schema is unavailable.

## Experiment 1 — preserve the old application artifact

Before changing the application, the pre-Issue-101 application was compiled and retained as:

    .local/issue-101/demo-api-schema-a

Its version was:

    efbfa61-schema-a

Against Schema A / migration version 2 it was verified to:

- report healthy;
- report ready;
- create Work Items;
- list Work Items.

This preserved a real old application artifact rather than reconstructing old behavior from memory.

## Experiment 2 — old application with newer Schema AB

While using the preserved old application, migration 000003 was applied.

The database moved from migration version:

    2 -> 3

The old application continued to:

- report healthy;
- report ready;
- create Work Items;
- list Work Items.

Rows created by the old application received:

    status = pending

from the database default.

Result:

    old application + Schema AB = compatible

This proves the additive migration can be applied before the new application.

## Experiment 3 — new application with Schema AB

The evolved application was run against migration version 3.

It successfully:

- read existing rows created under Schema A;
- observed those rows as `status=pending`;
- created a Work Item with omitted status and received `pending`;
- created a Work Item with `status=done`;
- rejected an unsupported status with HTTP 400.

Result:

    new application + Schema AB = compatible

## Experiment 4 — application rollback while keeping Schema AB

After the evolved application had created data including:

    status = done

the preserved old application artifact was started again without reverting migration 000003.

The old application remained able to:

- become ready;
- read the Work Items;
- create additional Work Items.

The new `status` column remained in the database.

Result:

    Schema AB + old application after rollback = compatible

This demonstrates an important operational boundary:

    application rollback != database rollback

For this additive change, application code can be rolled back while leaving the expanded schema in place.

## Experiment 5 — new application with insufficient Schema A

A fresh PostgreSQL environment was created and only migrations 000001 and 000002 were applied.

The resulting migration version was:

    2

The evolved application was then started against that insufficient schema.

Observed behavior:

    GET /health -> HTTP 200
    GET /ready  -> HTTP 503
    GET /items  -> HTTP 503
    POST /items -> HTTP 503

Persistence responses were:

    {"error":"persistence_unavailable"}

Application logs reported that the required `status` column did not exist.

The failed POST created no row.

The application did not automatically run migrations.

Migration version remained:

    2

Result:

    new application + Schema A = incompatible, but fails closed

The process remains alive, while readiness prevents it from claiming the persistence contract is usable.

## Compatibility matrix

The tested compatibility states are:

| Application | Schema A / v2 | Schema AB / v3 |
| --- | --- | --- |
| Old application | compatible | compatible |
| New application | fails closed | compatible |

This supports the tested rollout ordering:

    Schema A + old app
        ->
    apply additive migration
        ->
    Schema AB + old app
        ->
    deploy new app
        ->
    Schema AB + new app

It also supports the tested application rollback:

    Schema AB + new app
        ->
    rollback application
        ->
    Schema AB + old app

The experiment does not justify deploying the new application before migration 000003.

## Dirty migration experiment

A temporary local-only migration 000004 was created outside the tracked migration set.

It deliberately attempted:

    CREATE TABLE public.issue101_dirty_probe (...);

followed by an invalid function call.

The migration command failed non-zero.

golang-migrate then reported:

    4 (dirty)

and `schema_migrations` contained:

    version = 4
    dirty   = true

However:

    public.issue101_dirty_probe

did not exist after the failure.

For this PostgreSQL migration, the failed DDL was rolled back transactionally while golang-migrate retained the dirty migration marker.

The resulting state was therefore:

    physical schema = last known-good Schema AB / v3
    migration metadata = v4 dirty

## Dirty-state recovery

Before changing migration metadata, the physical database state was inspected.

Only after confirming that the failed migration had left no probe table and that Schema AB remained intact was the migration metadata explicitly reconciled with:

    force 3

The result became:

    version = 3
    dirty   = false

A subsequent normal:

    migrate-up

reported:

    no change

and migration version remained:

    3

This demonstrates the recovery principle:

    migration failure
        ->
    inspect physical database state
        ->
    determine last known-good version
        ->
    explicitly reconcile migration metadata
        ->
    verify normal migration operation resumes

The repository helper now exposes:

    ./tools/postgres-local migrate-force <version>

This command does not determine the correct version automatically.

The operator remains responsible for inspecting the database and deciding which version accurately represents the physical schema.

`migrate-force` must not be treated as:

    dirty -> automatically force previous version

That would be unsafe when a failed migration leaves partial or non-transactional changes behind.

## Runtime privilege experiment

After migration 000003, the runtime role was verified to have:

    SELECT(status) = allowed
    INSERT(status) = allowed

The following operations were deliberately attempted as `zero_to_prod_app` and denied:

    CREATE TABLE
    ALTER TABLE
    UPDATE work_items
    DELETE FROM work_items

Observed PostgreSQL errors included:

    permission denied for schema public
    must be owner of table work_items
    permission denied for table work_items

No test table or test column remained afterward.

Schema evolution therefore did not broaden the runtime role into a migration or administrative identity.

## Backup and restore regression

Issue #99 established a migration-first, data-only Work Item recovery contract.

Issue #101 verified that the expanded Schema AB remains compatible with that current-version recovery model.

A backup containing:

    status=pending
    status=done

was created.

The generated restore SQL contained:

    COPY public.work_items (id, title, created_at, status)

This shows that the table-data backup naturally includes the evolved column.

The active PostgreSQL data volume was then destroyed.

Recovery followed the existing responsibility boundary:

    recreate PostgreSQL
        ->
    apply repository migrations through version 3
        ->
    restore data-only Work Item backup

The restored rows preserved both status values.

The restored identity sequence also remained usable: after restoring IDs 1 and 2, the next inserted Work Item received ID 3.

This proves:

    Schema AB backup -> Schema AB restore

for the tested local workflow.

It does not prove:

    Schema AB backup -> older Schema A restore

Cross-version backup compatibility remains outside this experiment.

## Safe ordering decision

For this additive Work Item change, the tested safe order is:

1. deploy the schema expansion first;
2. verify Schema AB is usable;
3. deploy the new application;
4. if application rollback is required, roll the application back without immediately contracting the schema.

This ordering is supported by direct experiments with both application versions.

The new application must not be rolled out before the required schema exists.

## Schema B boundary

Issue #101 does not implement a destructive Schema B contraction.

Examples of future contraction would include:

- removing compatibility defaults;
- removing columns still required by an old application;
- changing values or constraints so old application behavior is invalid;
- removing old privileges relied on by a previous application version.

Such contraction requires a separate compatibility decision after old application versions are no longer part of the rollback window.

## What this experiment proves

Issue #101 demonstrates:

- additive schema expansion from Schema A to Schema AB;
- old-application compatibility with the expanded schema;
- new-application compatibility with the expanded schema;
- fail-closed behavior when the new application sees insufficient schema;
- application rollback without database rollback for the tested additive change;
- explicit dirty-migration observation and recovery;
- runtime least-privilege preservation after schema evolution;
- current-version backup/restore compatibility after the schema expansion;
- explicit migration/application responsibility separation.

## What this experiment does not prove

It does not prove:

- production zero-downtime migration;
- multi-instance rolling deployment behavior;
- concurrent old/new application traffic;
- destructive Schema B contraction;
- arbitrary forward/backward schema compatibility;
- automatic database rollback;
- safe `force` behavior without prior inspection;
- cross-version backup restore;
- managed PostgreSQL behavior;
- PostgreSQL major-version compatibility;
- high availability or replication;
- PITR or WAL recovery;
- large-dataset migration performance;
- lock-duration safety under production load;
- a representative comparison of alternative schema-evolution strategies.

## Learning level

This work supports L4 for the tested schema-evolution capability:

- a real additive strategy was implemented;
- multiple application/schema combinations were exercised;
- incompatible ordering was deliberately tested;
- rollback behavior was exercised;
- a dirty migration was induced deliberately;
- operator recovery was performed and verified;
- authorization boundaries were tested;
- recovery behavior was regression-tested.

It does not justify L5.

L5 would require comparing credible alternative schema-evolution approaches under meaningful constraints and making a defensible decision between them.

## Cost

Environment:

    LOCAL-FIRST

Additional AWS infrastructure:

    none

Expected additional AWS infrastructure cost:

    $0
