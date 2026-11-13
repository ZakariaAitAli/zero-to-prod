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

### Assess before resuming

A successful restore establishes PostgreSQL state only. RabbitMQ is not
restored with it, so it can hold messages that disagree with the restored
database. Keep the API, worker, and client requests stopped until both sides
have been assessed. The API embeds the outbox publisher: starting it resumes
publication, so assess PostgreSQL with SQL rather than through the API.

1. **PostgreSQL.** With the same project overrides, record restored sequence
   values and accepted jobs with their publication state:

   ```bash
   docker compose --project-name "$ZTP_COMPOSE_PROJECT_NAME" -f infra/local/compose.yaml exec -T postgres \
     psql -U zero_to_prod_admin -d zero_to_prod -c \
     "SELECT 'work_items' AS sequence, last_value FROM public.work_items_id_seq
      UNION ALL SELECT 'processing_jobs', last_value FROM public.processing_jobs_id_seq
      UNION ALL SELECT 'outbox_messages', last_value FROM public.outbox_messages_id_seq;" -c \
     "SELECT j.id AS job_id, j.work_item_id, o.published_at IS NOT NULL AS published
      FROM public.processing_jobs j JOIN public.outbox_messages o ON o.processing_job_id = j.id
      WHERE j.state = 'accepted' ORDER BY j.id;"
   ```

   Unpublished accepted jobs will be published when the API starts. Published
   accepted jobs depend on a message that may no longer exist.
2. **RabbitMQ.** Record queued and unacknowledged message counts:

   ```bash
   docker compose --project-name "$ZTP_RABBITMQ_COMPOSE_PROJECT_NAME" -f infra/local/rabbitmq/compose.yaml exec -T rabbitmq \
     rabbitmqctl -p zero_to_prod list_queues name messages_ready messages_unacknowledged consumers
   ```

3. **Stop condition.** Queue counts show how many messages exist, not which
   jobs they belong to, so they do not establish that resuming is safe.
   Queue assessment alone mitigates neither stale messages nor lost deliveries;
   apply the protocol-2 recovery procedure below before resuming. If a
   stale-message identity or missing-delivery discrepancy
   remains unresolved, keep the affected processing and new client requests
   stopped.
4. **Decide, then resume in order:** API publication, worker consumption, and
   finally client requests. Once the API is running, verify that `GET /items`
   returns recovered results with matching done state. Pending jobs and
   unpublished outbox rows resume from the restored recovery point.

### Delivery recovery with protocol version 2

The historical #118 experiment reproduced lost delivery and stale messages
acting on reused IDs. ADR 0009 adds a random delivery identity checked against
the restored outbox before processing, and an explicit offline redrive command.
Follow [the delivery recovery guide](../guides/restore-delivery-recovery.md)
before resuming API publication, worker consumption, and client admission.

Stop every API and worker process. Inspect accepted jobs with
`tools/work-items-recovery-local inspect`; after confirming the restored target,
run `tools/work-items-recovery-local redrive-after-restore` and retain its JSON
report. It returns delivery responsibility for all accepted jobs to their
existing outbox rows without creating jobs or resetting attempt counts.
Duplicate delivery is expected and remains idempotent under ADR 0003.

The [#130/#131 experiment](../experiments/issue-130-131-restore-recovery.md)
exercises both mitigations after destructive loss of an isolated PostgreSQL
volume. An old message cannot complete new work with reused IDs; a blocked
accepted job completes after offline redrive, and a subsequent duplicate leaves
its business state unchanged. This does not recover writes missing from the
snapshot or provide online reconciliation. Match schema and protocol versions
before starting runtime; historical migration-5 backups need migration 6 after
restore, as explained in the guide.

## Scope and evidence

`scripts/test-async-result-recovery.sh` uses temporary databases to check legacy
migration refusal without mutation, old-archive rejection, and restoration of
all four tables with a completed result. CI runs it after integration tests.
Historical recovery evidence remains in Sprint 03 and ADR 0001.

The crash-consistency experiment exercises destructive restore with live API and
worker processes in an isolated lab; its evidence is in
[`evidence/issue-118/`](../../evidence/issue-118/).

This is snapshot recovery. Writes after the backup point may be lost. No PITR,
production RPO/RTO, broker disaster recovery, or sustained-operation claim is
made.
