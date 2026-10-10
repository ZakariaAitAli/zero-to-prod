# Sprint 03 — Stateful Reference System

Milestone: Sprint 03 — October 2026: Stateful Reference System.

This page indexes Sprint 03 and summarizes its outcome. The linked records
remain the factual source for what each issue did and observed.

## Status

Status as of the Issue #119 verification on 2026-10-10: the engineering work
was complete; the Issue #119 reconciliation was awaiting review, final CI, and
merge; and the milestone was still open. Final baseline CI remains an
outstanding #119 acceptance criterion until it passes. The reference-workload
decision takes effect on merge. Check the milestone and the #119 pull request
for later status.

## Outcome

Sprint 03 turned Work Items from a stateless deployment target into a
local-first stateful reference system:

- PostgreSQL holds Work Items, processing jobs, the transactional outbox, and
  title-analysis results, with explicit migrations and separate administration,
  migration, API, and worker identities.
- The API durably accepts processing in PostgreSQL before publication; an
  API-side outbox publisher sends confirmed messages to RabbitMQ.
- A manual-acknowledgement worker commits one deterministic title-analysis
  result, the Work Item `done` transition, and the job `succeeded` transition in
  one transaction, then acknowledges the message. Business state and execution
  state are separate models.
- A separate browser Web UI lists and creates Work Items, submits processing,
  and shows job state and results.
- Failure experiments covered schema compatibility, broker and database
  outages, real process crashes at observed points, uncertain commits, duplicate
  and concurrent delivery, and destructive restore. In the crash, outage, and
  delivery experiments, completion stayed atomic and redelivery recovered the
  work. Snapshot restore recovered completed and unpublished accepted work but
  also reproduced the two open recovery limitations listed below. Exactly-once delivery is
  not claimed.

The current system is described in the
[Work Items architecture](../architecture/work-items.md).

## Decision: Work Items becomes a stable reference workload

Work Items is a reference workload for future architecture experiments, not a
product under continued feature development. It changes only when a future
experiment needs a real capability or exposes a concrete correctness gap. New
product features are not part of Sprint 03 and are not planned for Work Items.

No ADR records this decision: there were no credible competing architectures to
compare, only the choice between continuing feature work and stopping it. The
decision is recorded here and in the architecture document.

## Milestone goals and evidence

| Goal | Evidence |
| --- | --- |
| Migration foundation and database lifecycle | [Local PostgreSQL guide](../guides/local-postgresql.md); [migrations](../../apps/work-items/migrations/); issue [#95](https://github.com/ZakariaAitAli/zero-to-prod/issues/95) |
| PostgreSQL-backed state and dependency-aware readiness | [Local PostgreSQL guide](../guides/local-postgresql.md); [local Work Items guide](../guides/local-work-items.md); issue [#97](https://github.com/ZakariaAitAli/zero-to-prod/issues/97) |
| Backup, restore, and destructive recovery | [Backup and destructive recovery record](postgresql-backup-restore.md); [historical runbook](runbook.md); issue [#99](https://github.com/ZakariaAitAli/zero-to-prod/issues/99) |
| Schema evolution and application/schema compatibility | [Schema evolution record](postgresql-schema-evolution.md); issue [#101](https://github.com/ZakariaAitAli/zero-to-prod/issues/101) |
| Asynchronous processing, RabbitMQ, transactional outbox, retry and terminal semantics | [Reliable asynchronous processing record](reliable-async-processing.md); issue [#103](https://github.com/ZakariaAitAli/zero-to-prod/issues/103) |
| Explicit recovery boundary for accepted work | [ADR 0001](../adr/0001-work-items-async-recovery-boundary.md); [accepted-work recovery experiment](../experiments/issue-111-recovery-model.md); issue [#111](https://github.com/ZakariaAitAli/zero-to-prod/issues/111) |
| Browser Web UI and observable job state | [ADR 0002](../adr/0002-work-items-web-ui-boundary.md); [Web UI](../../apps/work-items/web/README.md); issue [#113](https://github.com/ZakariaAitAli/zero-to-prod/issues/113) |
| Meaningful worker effect, business vs execution state, result ownership and storage decision | [ADR 0003](../adr/0003-work-items-async-success-semantics.md); issue [#116](https://github.com/ZakariaAitAli/zero-to-prod/issues/116) |
| Result implementation and atomic business completion | [Result implementation validation](../experiments/issue-117-result-validation.md); issue [#117](https://github.com/ZakariaAitAli/zero-to-prod/issues/117) |
| Idempotency, crash consistency, and recovery under failure | [Crash-consistency experiment](../experiments/issue-118-crash-consistency.md); [evidence](../../evidence/issue-118/); [current recovery runbook](../runbooks/work-items-recovery.md); issue [#118](https://github.com/ZakariaAitAli/zero-to-prod/issues/118) |
| Repository correctness and development hygiene | [AWS backend retirement evidence](../../evidence/aws-retirement/terraform-backend-retirement.md); issues [#105](https://github.com/ZakariaAitAli/zero-to-prod/issues/105) and [#108](https://github.com/ZakariaAitAli/zero-to-prod/issues/108); PRs [#110](https://github.com/ZakariaAitAli/zero-to-prod/pull/110) (documentation organization) and [#115](https://github.com/ZakariaAitAli/zero-to-prod/pull/115) (repository audit) |
| Reference-system completion boundary | This page; issue [#119](https://github.com/ZakariaAitAli/zero-to-prod/issues/119) |

## Capability levels

No level changes in Issue #119; it adds no new behavioral evidence. The
[capability baseline](../learning/current-capability-baseline.md) remains
authoritative. Sprint 03 work is reflected there at the levels its evidence
supports: for example, backup and restore, messaging, and distributed-system
behavior at L4, and the browser application at L3. No L5 or L6 claim is made.

## Open limitations

These remain open and unmitigated. They are documented, not fixed:

- [#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130): after a
  PostgreSQL restore, a job whose message was already delivered stays
  `accepted` with no message, and new requests for its item are refused.
- [#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131): after a
  PostgreSQL restore, a stale queued message can target IDs that new work
  reuses, and the worker cannot tell them apart.
- Frontend behavior has no automated tests; CI runs lint and build only.

See the [current recovery runbook](../runbooks/work-items-recovery.md) for the
assessment steps and stop condition after a restore.

## Issue #119 verification

Run on 2026-10-10 between 14:36 and 14:40 UTC. Source revision: `main` at
`6f10355` plus the uncommitted Issue #119 documentation changes; application
code, migrations, tools, and CI configuration were unchanged from `main`.

Environment: an isolated, freshly created lab using the guide's
[legacy-data workflow](../guides/local-work-items.md#start-the-current-system-next-to-preserved-legacy-data):
PostgreSQL project `zero-to-prod-119` on port 55435, RabbitMQ project
`zero-to-prod-119-rabbitmq` on ports 5674/15674, API on 8080, Vite on 5173.
Existing lab volumes were recorded before and compared after.

### Automated checks

| Check | Outcome |
| --- | --- |
| `postgres-local start`, `build-migrate`, `migrate-up` | Migrations 1–5 applied; version 5 |
| `rabbitmq-local start` | Healthy; runtime topology ensured |
| `work-items-api-local run`, then `verify` | `/health`, `/ready`, `/version` passed |
| `go test ./...` without integration URLs; `go vet` with and without `-tags crashexperiment` | Passed |
| API and worker integration suites against the isolated lab (queue purged between them, as in CI) | Passed, no skips |
| `pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm build` | Passed; no tracked files changed |
| CI policy scripts and `actionlint` | Passed |

### Scripted behavior checks (`curl`)

| Check | Outcome |
| --- | --- |
| Create Work Item | `201`, status `pending` |
| Create with `"status": "done"` / `"pending"` | `400 invalid_status` / `201` |
| Process, poll job, list items | Job `succeeded`, `attempt_count` 1; item `done` with result: 17 characters, 3 words |
| Process a done item | `409 work_item_already_done` |
| Process twice with the worker stopped | `202`, then `409 processing_already_active` naming the same job |
| Stop PostgreSQL, then start it | `/health` 200 and `/ready` 503 while stopped; `/ready` 200 again in the same API process |
| Restart worker | The queued job completed |
| `SIGTERM` to API and worker | Both logged graceful shutdown |
| Runbook assessment commands | Ran as written; no accepted jobs, empty queue |

### Manual browser check

Exercised by hand in a browser against the running lab; this is not an
automated test. The list showed results for done items with processing
disabled for them. Creating "Browser happy path check" and processing it showed
`succeeded` and "24 characters, 4 words"; the result persisted after reload. No
console errors appeared.

### Cleanup

| Check | Method | Outcome |
| --- | --- | --- |
| `stop` keeps data | `rabbitmq-local stop` and `postgres-local stop`; `docker ps -a` and `docker volume ls`; then `start` both; a `psql` count of `work_items` and `work_item_results` before stopping and after starting | Both containers `Exited (0)` with both #119 volumes still listed; 10 Work Items and 3 results before and after |
| `destroy` removes the #119 lab | `rabbitmq-local destroy` and `postgres-local destroy`; then `docker volume ls`, `docker ps -a`, and `docker network ls` filtered for `zero-to-prod-119` | Tools reported removing the two #119 volumes and networks; no #119 volume, container, or network remained |
| Other lab volumes still present | `docker volume ls --format '{{.Name}}' \| grep zero-to-prod` captured before the lab was created and after it was destroyed, compared with `diff` | Same five volume names before and after: `zero-to-prod-local_postgres_data`, `zero-to-prod-117_postgres_data`, `zero-to-prod-rabbitmq_rabbitmq_data`, `zero-to-prod-118_postgres_data`, `zero-to-prod-118-rabbitmq_rabbitmq_data` |

Only volume names were compared. Labels, mounts, metadata, and contents of the
other volumes were not inspected; the #119 commands never targeted them.

### Not run

- The guide's default commands were not run verbatim: on this machine the
  default projects hold pre-migration-5 data that must be preserved. The same
  commands ran with the documented overrides.
- The crash-consistency experiments were not rerun; Issue #119 changes no
  behavior. Their evidence is in [`evidence/issue-118/`](../../evidence/issue-118/).
- GitHub CI for the Issue #119 changes had not run at verification time
  (2026-10-10); it is not part of this record.

## Sprint records

- [PostgreSQL backup and destructive recovery](postgresql-backup-restore.md)
- [PostgreSQL schema evolution](postgresql-schema-evolution.md)
- [Reliable asynchronous processing](reliable-async-processing.md)
- [Historical PostgreSQL backup and recovery runbook](runbook.md)
- Focused experiments: [accepted-work recovery](../experiments/issue-111-recovery-model.md), [result implementation validation](../experiments/issue-117-result-validation.md), [crash consistency](../experiments/issue-118-crash-consistency.md)
