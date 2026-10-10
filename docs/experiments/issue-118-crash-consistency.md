# Issue #118 — Crash consistency, duplicate delivery, and recovery

## Problem

The API accepts a processing job in PostgreSQL, then an outbox publisher sends
its message to RabbitMQ. The worker calculates character and word counts from
the stored Work Item title. Success must commit the result, the item becoming
`done`, and the job becoming `succeeded` in one database transaction. RabbitMQ
acknowledgement follows that commit.

[ADR 0003](../adr/0003-work-items-async-success-semantics.md) defines one
PostgreSQL-owned result per Work Item and a required failure model. Issue #117
implemented it and validated the implementation scope, but it did not show how
real processes behave when they are killed, paused, shut down, or disconnected
at specific points, or what snapshot recovery does to in-flight messaging.

Question: across actual process crashes, database interruptions, duplicate and
concurrent deliveries, and destructive restore, does the committed success
invariant hold, and does redelivery recover safely?

## Hypothesis

- Crashes before COMMIT roll every success write back; the delivery is requeued.
- A COMMIT interrupted from the client side may commit or roll back; either
  outcome is all-or-nothing, and redelivery inspects state before reprocessing.
- After COMMIT, a lost ACK causes redelivery that recognizes terminal success.
- Concurrent or duplicate deliveries commit at most one result.
- PostgreSQL outages cause requeues, not recorded processing failures.
- Snapshot recovery loses post-backup effects; the existing missing-delivery
  limitation applies, and stale broker messages may meet rewound identities.

## Environment

LOCAL-FIRST. Provider semantics are not part of the question.

- PostgreSQL 18.6: Compose project `zero-to-prod-118`, port 55434, fresh volume.
- RabbitMQ 4.3.6: Compose project `zero-to-prod-118-rabbitmq`, ports 5673/15673,
  via the new `ZTP_RABBITMQ_COMPOSE_PROJECT_NAME` override.
- Native API and worker binaries built from this branch, run as child processes.
- Existing lab volumes (`zero-to-prod-local`, `zero-to-prod-117`, default
  RabbitMQ) and `.local/postgres-backups/` were not used and remained present.

Runs recorded here: 2026-10-10, from a freshly provisioned isolated lab.
The publisher-crash, database-outage, and snapshot-recovery records contain
later runs from the same day. Each evidence file records its capture time.

## Implementation

No production code changed. Changes:

- Deterministic integration tests (run in CI): single-result enforcement,
  six overlapping completions, 32 overlapping HTTP acceptance requests, injected
  retry exhaustion followed by a successful new job, injected transient and
  exhausted failures through real RabbitMQ requeues, duplicate publication, and
  business-state assertions in the existing redelivery/restart tests.
- An opt-in harness behind the `crashexperiment` build tag
  (`apps/work-items/worker/*experiment*_test.go`). CI only vets it.

### Synchronization and instrumentation

The harness signals a process only after observing its blocking point; polling
observes conditions and never substitutes for them.

| Need | Mechanism (test-only, isolated database only) |
| --- | --- |
| Before first write | Harness holds the per-item advisory lock; worker backend observed waiting on it with no transaction ID. |
| After result and item writes | Harness holds `FOR NO KEY UPDATE` on the job row; worker observed in the guarded job `UPDATE`; `pageinspect` shows heap tuples with the worker's `xmin` in `work_item_results` and `work_items`, none yet in `processing_jobs`. |
| Inside COMMIT | Deferred constraint trigger on `work_item_results` waits on a harness-held advisory lock; worker observed executing `commit`, blocked by the harness. |
| Outbox crash window | `BEFORE UPDATE` trigger on the `published_at` transition waits on a harness-held lock; API backend observed in that `UPDATE` after RabbitMQ already holds the confirmed message. |
| Broker state | `rabbitmqctl list_queues` inside the validated container (ready, unacknowledged, consumers), not lagging management statistics. |

### Why observed locks and triggers

Three ways to place a crash were considered:

- **Timing-based crashes** (sleep, then signal) cannot show where the process
  was. A run that kills too early or too late still passes, so the evidence
  would not prove which window was exercised.
- **Crash hooks in the production binaries** would place crashes exactly, but
  they add fault-injection paths to shipped code and test a modified program.
  This issue keeps production behavior unchanged.
- **Database locks and test-only triggers** hold the unmodified binaries at a
  real wait inside PostgreSQL. The harness reads that wait, the transaction ID,
  and the tuples already written before it signals anything.

The third option was chosen. Its cost is that every window is wider than at
natural timing; see the remaining unknowns.

### Privileges and isolation

Observation and instrumentation need a PostgreSQL superuser. Other roles'
wait events, queries, and transaction IDs are hidden from ordinary roles;
`pageinspect`, `pg_terminate_backend` on another role's backend, and triggers
on application tables all need elevated rights. Those same rights could stall,
terminate, or corrupt a real lab. The harness therefore refuses non-isolated
projects and protected ports, proves the database URL and the container are the
same cluster, re-checks targets before each destructive step, and requires an
explicit opt-in naming the project before destroying a volume. Credentials are
local lab defaults and are redacted from evidence.

Destructive actions re-validate the Compose project prefix, labels, the
container's host-port binding, and (PostgreSQL) that the URL endpoint and the
container report the same `system_identifier`. Volume destruction also checks
the volume label and mount, and protected volumes are compared before and after.

Classification used below: **ACTUAL** = a real OS process is signalled;
**EXTERNAL** = a real infrastructure fault (container stop/kill, backend
termination); **SIMULATED** = test-only injection (processor failures, harness
publication).

## Results

Evidence: [`evidence/issue-118/`](../../evidence/issue-118/), one JSON file per
experiment with expected/observed checks, synchronization observations, process
signals and exit statuses, and sanitized log excerpts.

| Evidence | Experiment | Class | Expected | Observed |
| --- | --- | --- | --- | --- |
| [Crash before writes](../../evidence/issue-118/e1-crash-before-result-insertion.json) | SIGKILL while waiting for the item lock, before any write | ACTUAL | Untouched; requeue; restart completes | Backend had no xid; queue went unacked→ready 1; item pending/accepted/attempt 0; restart: done once, ACKed. |
| [Crash after writes](../../evidence/issue-118/e2-crash-after-writes-before-commit.json) | SIGKILL at guarded job `UPDATE` after result insert and item update | ACTUAL | All writes roll back | Blocked xid had written 1 result tuple and 1 item tuple, 0 job tuples; after kill and release: pending/accepted/attempt 0, no result. Aborted tuples remain physically present and invisible. |
| [Client death during commit](../../evidence/issue-118/e3a-kill-client-during-commit.json) | SIGKILL during COMMIT, `client_connection_check_interval=0` | ACTUAL | Either outcome, all-or-nothing | Backend kept waiting for 2 s after client death and **committed** on gate release. Redelivery ACKed terminal success; attempt 1. |
| [Client death with connection checks](../../evidence/issue-118/e3b-kill-client-during-commit-connection-check.json) | SIGKILL during COMMIT, `client_connection_check_interval=100ms` | ACTUAL | Either outcome | Backend detected the dead client before release and **rolled back**. Restart processed once. |
| [Backend termination during commit](../../evidence/issue-118/e3c-terminate-backend-during-commit.json) | `pg_terminate_backend` during COMMIT | EXTERNAL | Either outcome | **Rolled back**. Worker logged “outcome may be uncertain”, requeued, and the same process completed once. |
| [Crash before acknowledgement](../../evidence/issue-118/e4-crash-after-commit-before-ack.json) | Commit visible while worker is SIGSTOPped, then SIGKILL | ACTUAL | Success survives; redelivery terminal | Harness connection saw done + result + succeeded while `/proc` state was `T` and the delivery was unacked; after kill ready=1 (no ACK ever sent); restart ACKed without changing state. |
| [Publisher death with statement completion](../../evidence/issue-118/e5a-outbox-crash-statement-completes.json) | API SIGKILL after broker confirm, mark-published statement left to the server | ACTUAL | Observe | Server completed the in-flight `UPDATE`: `published_at` durable, 1 attempt, no republication within 3.2 s of restart. The window did **not** open. |
| [Publisher death with statement termination](../../evidence/issue-118/e5b-outbox-crash-window-duplicate.json) | API SIGKILL after broker confirm, plus termination of the publication-marker statement | ACTUAL + EXTERNAL | Required: `published_at` NULL with the confirmed message queued, then duplicate publication | `published_at` NULL with 1 confirmed message in RabbitMQ; restarted API republished (2 messages, 2 attempts); worker committed once and ACKed both. |
| [Concurrent duplicate deliveries](../../evidence/issue-118/e6-concurrent-duplicate-delivery.json) | Two worker processes hold copies of one message | ACTUAL; duplicate publication SIMULATED | One completion | One snapshot showed both backends waiting on the item lock (B blocked by harness and A); 2 unacked deliveries; after release: one result, attempt 1, both ACKed, no requeues. |
| [Graceful shutdown](../../evidence/issue-118/e10-graceful-shutdown.json) | SIGTERM idle and in flight | ACTUAL | Observe | Idle: exit 0 in 2 ms. In flight (lock wait): exit 0 in 5 ms, no backend left, message ready again; item untouched; restart completed once. |
| [Database outage](../../evidence/issue-118/e11a-postgres-outage-retry.json) | `docker stop` PostgreSQL with a message arriving | EXTERNAL | Requeue without recorded failure | 11 session restarts in 10 s, intervals 1005–1010 ms (median 1005); worker stayed up; success observed from a separate connection 640 ms after invoking `docker start`; attempt 1. |
| [Database crash during commit](../../evidence/issue-118/e11b-postgres-crash-during-commit.json) | `docker kill` PostgreSQL during COMMIT (worker paused) | EXTERNAL | Either outcome | **Rolled back** after crash recovery; on SIGCONT the worker logged an uncertain commit, requeued, and completed once. |
| [Snapshot recovery](../../evidence/issue-118/e12-snapshot-recovery.json) | Destroy isolated volume, migrate, restore snapshot | EXTERNAL; processes ACTUAL | See below | See below. |

Deterministic integration results (CI): processor failures are **SIMULATED**
with test-only processors. Three failures mark the job `failed` (attempt 3,
`finished`, `processing_failed`) with the item pending and no result; a later
job completes and owns the result while the failed job is unchanged. Two
injected failures followed by success through real RabbitMQ redeliveries end
`done` with attempt 3. 32 concurrent HTTP requests, after every pooled API
connection was observed waiting on the item lock, produced one 202 and 31
`409 processing_already_active` naming the same job, with one job and one
outbox row; requests for a done item all returned `work_item_already_done`.

### Snapshot recovery

The backup captured three cases: an item already completed, an accepted job
whose message was published, and an accepted job whose outbox message was still
unpublished because RabbitMQ was stopped. After the backup, both accepted jobs
completed. Another item was created and published after the backup, leaving its
message queued without a consumer. The isolated PostgreSQL volume was then
destroyed, recreated, migrated, and restored.

- **Completed before backup:** the item, result, and succeeded job restored
  identically.
- **Unpublished at backup:** the restored outbox row was republished and the
  worker completed the item. Recovery of accepted unpublished work still holds
  with the result included in the database backup.
- **Published at backup, consumed afterward:** the job restored as accepted
  with `published_at` set, but its message had already been consumed. It
  remains blocked without operator intervention or a redrive mechanism. A new
  request returns `409 processing_already_active`. This is ADR 0003's known
  missing-delivery limitation, tracked in
  [#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130).
- **Stale message after sequence rewind:** all three identity sequences
  returned to their backup values. A message for post-backup work (job 195,
  item 183) remained queued in RabbitMQ. New work created after restore
  received those same IDs; the experiment requires this collision and fails
  without it. Before the new job's own outbox message was published, the worker
  consumed the stale message and completed the new job. Its result used the
  new item's title because the worker reads input from PostgreSQL.

Triggering conditions for the alias: (1) work accepted and published after the
recovery point; (2) its message survives in RabbitMQ while PostgreSQL is rolled
back; (3) new work reaches the identical `(job_id, work_item_id)` pair before the
stale message is consumed. By code inspection (not exercised): if only `job_id`
matched, the worker rejects the message as an identity mismatch; if the job did
not exist yet, it rejects it as unknown — dropping it, which is harmless for
lost work.

#### Integrity implications

This is an extra, misattributed delivery, not a missing one.

- **Result integrity held by construction, not by design.** The result was
  correct only because messages carry identities and the worker reads its input
  from PostgreSQL. The worker does verify that the message's job exists and
  belongs to the named Work Item, but after a restore reuses both IDs, an old
  message and the new work satisfy those checks identically, so the worker
  cannot tell them apart.
- **Provenance is wrong.** The new job completed before its own message was
  published, so delivery history no longer explains the outcome.
- **Failures would be misattributed.** A failing stale delivery would consume
  the new job's retry budget.
- **The risk grows with the message contract.** If messages carried business
  data or processing versions, or triggered external effects, a stale message
  would apply them to unrelated work.

Tracked in [#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131).

## Analysis

- Client-observed COMMIT failure is not evidence of rollback. With connection
  checks disabled, PostgreSQL committed after the worker died. With connection
  checks enabled, backend termination, or a database crash, the transaction
  rolled back. Inspecting job state on redelivery handled every observed
  outcome. The commit gate widens a window that is normally short but non-zero.
- Killing the publisher did not produce a duplicate when PostgreSQL finished
  its publication-marker update. Terminating that statement left a confirmed
  message with no durable marker, so restarting the publisher produced a
  second copy. Worker-side terminal-state inspection preserved one result.
- During a database outage the worker requeues and reconnects about once per
  second indefinitely, logging each cycle; there is no backoff growth. Outages
  never increment `attempt_count`, so they cannot exhaust retries.
- Recovery after the outage is reported as observation latency: elapsed time
  from just before `docker start` is invoked until a separate harness
  connection reads committed success, polling every 10 ms. It is an upper bound
  that includes PostgreSQL startup and the worker's next retry. `finished_at`
  is not used because `CURRENT_TIMESTAMP` records transaction start, not
  durable completion.
- Graceful shutdown cancels the in-flight PostgreSQL wait promptly; the
  delivery remained available. Whether it returned by explicit nack or by
  connection closure was not distinguished.

## Learning

Can now be explained, tested, and diagnosed: where each crash point leaves
PostgreSQL and RabbitMQ state; why client-side commit uncertainty needs state
inspection; how `client_connection_check_interval` changes the observed commit
outcome; how outbox duplicates arise; and why database-only snapshot recovery
interacts with an independently durable broker.

## Remaining unknowns

- No exactly-once delivery or execution is claimed; computation can repeat.
- Windows were widened by test-only locks and triggers; natural timings,
  load, and multi-node PostgreSQL/RabbitMQ behavior were not measured.
- Broker crash/restart during delivery, heartbeat expiry while a worker is
  paused, and RabbitMQ disaster recovery were not exercised.
- Production RPO/RTO, PITR, and sustained operation are not established.
- No mitigation for the stale-message alias or the blocked item was
  implemented or tested.

## Decision

No architectural decision changes in this issue. ADR 0003 records a dated
pointer to this evidence. Follow-ups, not implemented here:

- [#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130): redrive
  accepted jobs whose delivery was lost across a restore (ADR 0003 already
  scopes this out).
- [#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131): prevent
  stale broker messages from targeting reused IDs. Candidate directions include
  restore-time queue quarantine, advancing sequences past the pre-loss
  high-water mark, and a non-reusable delivery identity checked by the worker.

## Cost and cleanup

No cloud resources are used. The isolated lab adds two containers. Measured
idle memory was about 46 MiB for PostgreSQL and 188 MiB for RabbitMQ, and the
volumes held about 66 MB and 0.4 MB. A full run of the process and outage
experiments took about 90 seconds, and snapshot recovery about 40 seconds.

The harness is **kept** as opt-in scaffolding; CI vets it. Whoever starts the
isolated lab tears it down; the harness never removes it. Instrumentation
objects (`issue118` schema, `pageinspect`, gate triggers) exist only in the
isolated database and are removed by destroying it. Child processes are killed
when each experiment ends; on failure the isolated queue is purged. Tear down
the isolated lab with the commands in the
[local guide](../guides/local-work-items.md#isolated-crash-consistency-lab).
