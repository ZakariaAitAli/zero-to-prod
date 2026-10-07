# Issue #117 — Title-analysis implementation validation

Environment: LOCAL-FIRST, isolated PostgreSQL Compose project `zero-to-prod-117`
on port 55433, repository RabbitMQ lab, native Go API/worker, Vite frontend.
The legacy default PostgreSQL volume was preserved.

Review follow-up: permission denial is asserted as SQLSTATE 42501, transaction
support is part of the worker database interface, and completion decisions have
database-free tests. Item locks use a shared two-int namespace with hashed bigint
IDs; hash collisions cause extra serialization only. Inconsistent non-pending
business state rejects delivery, while transient completion guards requeue.

## Observed behavior

| Check | Outcome |
| --- | --- |
| Browser create → process → poll → refresh | Review API design became done with analysis v1, 17 code points, 3 words, and its producing succeeded job. Result persisted across reload; no page JavaScript errors. |
| Stale UI list after completion elsewhere | Clicking Process received the done conflict and refreshed the row to its persisted result without leaving an error. |
| HTTP completed processing request / direct done creation | 409 work_item_already_done / 400 invalid_status. |
| Two concurrent acceptance requests | One accepted job/outbox row; the other receives the same active job identity as a conflict. |
| Duplicate completion | Terminal outcome retained; attempt count and result remained unchanged. |
| Suppressed guarded job update | Result insertion and done transition rolled back; item pending, job accepted, no result. |
| Partial SQL business success | Database rejected a done item alone, succeeded job alone, and result alone. |
| Retry success | Existing retry test now observes done after successful completion. Retry exhaustion/redelivery tests continue passing. |
| Populated legacy migration | Actual golang-migrate execution refused before schema mutation, recorded dirty version 5, and force 4 restored clean version-4 metadata; existing row survived. |
| Historical archive | Validation explicitly rejected it as pre-#117. |
| Current logical archive | Four application tables and three sequence states restored consistently in one transaction into migration-5 schema. |

Reproducible checks are in API/worker integration tests and
`scripts/test-async-result-recovery.sh`; CI runs them with role-specific
credentials. Local API and worker suites passed with all integration environment
variables set and no skipped tests. Dependency-free Go tests, go vet, frontend
lint/build, CI policy tests, actionlint, and the API container build passed.

`pg_dump` reports circular foreign-key warnings for the new success references.
The initially deferred constraints and single-transaction restore passed the
round-trip check without disabling triggers or constraints.

This validates the implementation scope. It does not replace #118's deliberate
process-crash, outage, restart, and recovery experiment matrix or establish
exactly-once delivery, production RPO/RTO, or sustained operation.
