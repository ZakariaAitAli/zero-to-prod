# Recover delivery responsibility after a local snapshot restore

This guide covers #130/#131 with ADR 0009 and message protocol version 2.
Use it after restoring a database snapshot. It is not an online retry tool.

1. Stop **all** API and worker processes and their supervisors. Keep admission
   stopped until recovery is complete. Preserve the broker and a copy of the
   backup; do not purge unrelated queues. Record the target Compose project,
   database, broker, backup identity, and operator in the incident record.
2. Restore using the existing migration-first, empty-target backup runbook.
   Match the snapshot schema. For a migration-5 backup restore into schema 5
   first, then apply migration 6 while runtime remains stopped. For a
   migration-6 backup restore into schema 6. Never run old API/workers against
   the new protocol. Do not use a database-only restore as a claim of zero RPO.
3. Point `RECOVERY_DATABASE_URL_FILE` at the migrator credential file and set
   `ZTP_POSTGRES_DB` to the exact target database name. The file may contain
   the verified-TLS URL used by a security lab; it must match its trust files.
   Local development defaults apply only when neither credential variable is
   supplied. Avoid placing credentials in a shell history or command argument.
4. Run `tools/work-items-recovery-local inspect`. Save the JSON report. This
   enumerates accepted jobs, not proven lost messages. Published outbox rows
   with accepted jobs are candidates for loss; no age threshold is used.
5. Run `tools/work-items-recovery-local redrive-after-restore` and save its JSON
   report. Any unexpected runtime connections, wrong database, invalid graph,
   missing identity, or lock failure stops the operation without partial
   redrive. If commit confirmation is lost, inspect before retrying. Repeating
   an offline redrive is safe but can cause more duplicate deliveries.
6. Start version 2 API and workers. The publisher retransmits accepted jobs
   using the same delivery identity. Observe accepted jobs reaching a terminal
   state, outbox publication, queue depth, and errors. Confirm Work Item result,
   `done` state, and succeeded job agree (ADR 0003). Reconcile intentionally
   failed jobs separately; this command never converts loss into failure.
7. Keep the reports with the restore record. A completed job is not a count of
   broker deliveries: legitimate redelivery may have occurred. Mismatched
   identities are rejected, never processed as new jobs. Per-rejection runtime
   telemetry remains part of #122; the current loop does not log every
   rejection.

The command checks connections using the repository's default runtime role
names. Custom roles, separate supervisors, and future external side effects
need an updated fencing/runbook decision. Database session checks are not a
substitute for stopping the runtime. The JSON report contains IDs, not titles,
credentials, message bodies, or database URLs.
