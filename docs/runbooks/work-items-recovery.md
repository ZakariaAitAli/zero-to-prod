# Work Items result recovery

The current logical recovery boundary is `work_items`, `processing_jobs`,
`outbox_messages`, `work_item_results`, and sequence state for the three
identity-bearing tables. Migration 5 defines the result schema.

## Preserve legacy data

Migration 5 fails before schema changes when any application table has data.
It does not invent results for historical succeeded jobs or client-created done
items. Provision a separate empty database/Compose project as described in the
[local guide](../guides/local-work-items.md). Keep historical databases and
backups; recover them using the matching historical schema and application.
### Clear the expected migration-5 refusal

`golang-migrate` records version 5 as dirty before running its SQL. The expected
empty-table precheck refusal therefore leaves metadata at `5 (dirty)` even
though application schema and data remain at version 4.

Before clearing it, confirm the error was specifically `requires an empty
application database`, verify the schema still matches migrations 1–4 (no
`work_item_results` table, new index, or generated result columns), and inspect
the retained rows. Then, with the legacy project's environment overrides:

```bash
./tools/postgres-local migrate-version
./tools/postgres-local migrate-force 4
./tools/postgres-local migrate-version
```

Force 4 restores the metadata to the unchanged schema; it does not change data.
Do not force 5, and do not rerun migrate-up against that populated legacy
project. Use the historical application there and provision a separate empty
project for migration 5. For any other migration error, inspect actual schema
and data before choosing a recovery action; force 4 is not a general repair.

## Backup and restore

Keep the same `ZTP_COMPOSE_PROJECT_NAME`, `ZTP_POSTGRES_PORT`, and
`ZTP_POSTGRES_DB` overrides used to start the target lab. Stop API/worker writes
and publishing during restoration.

```bash
./tools/postgres-backup-local create .local/postgres-backups/results.dump
./tools/postgres-backup-local validate .local/postgres-backups/results.dump
```

A current archive has four TABLE DATA entries and three SEQUENCE SET entries.
Pre-#117 archives are rejected with an explicit compatibility error before
restoring application data. Validation checks archive scope, not application
correctness or compatibility with every future schema.

Provision an empty target, apply all migrations explicitly, then restore:

```bash
./tools/postgres-local migrate-up
./tools/postgres-backup-local restore .local/postgres-backups/results.dump
```

The target must have all four tables and no application data. Restore uses one
transaction. Deferred success foreign keys are checked at commit, preserving
the result/item/job relationship. `pg_dump` may warn about circular foreign
keys: the repository's single-transaction restore was verified with these
initially deferred constraints. Do not disable constraints or triggers.

Verify `GET /items` returns recovered results and matching done state, inspect
job state, then resume API publication and worker consumption. Pending jobs and
outbox responsibilities must resume from the restored recovery point.

## Scope and evidence

`scripts/test-async-result-recovery.sh` uses temporary databases to check legacy
migration refusal without mutation, old-archive rejection, and restoration of
all four tables with a completed result. CI runs it after integration tests.
Historical recovery evidence remains in Sprint 03 and ADR 0001.

This is snapshot recovery. Writes after the backup point may be lost. No PITR,
production RPO/RTO, broker disaster recovery, or sustained-operation claim is
made. Broader result-processing crash experiments are scoped to #118.
