# Issue #118 — Crash consistency, duplicate delivery, and recovery

## Problem

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

Runs recorded here: 2026-10-10, from a freshly provisioned isolated lab. After
review, E5a, E5b, E11a, and E12 were tightened and rerun in the same lab; their
evidence files are from that rerun.

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

| # | Experiment | Class | Expected | Observed |
| --- | --- | --- | --- | --- |
| E1 | SIGKILL while waiting for the item lock, before any write | ACTUAL | Untouched; requeue; restart completes | Backend had no xid; queue went unacked→ready 1; item pending/accepted/attempt 0; restart: done once, ACKed. |
| E2 | SIGKILL at guarded job `UPDATE` after result insert and item update | ACTUAL | All writes roll back | Blocked xid had written 1 result tuple and 1 item tuple, 0 job tuples; after kill and release: pending/accepted/attempt 0, no result. Aborted tuples remain physically present and invisible. |
| E3a | SIGKILL during COMMIT, `client_connection_check_interval=0` | ACTUAL | Either outcome, all-or-nothing | Backend kept waiting for 2 s after client death and **committed** on gate release. Redelivery ACKed terminal success; attempt 1. |
| E3b | Same, with `client_connection_check_interval=100ms` | ACTUAL | Either outcome | Backend detected the dead client before release and **rolled back**. Restart processed once. |
| E3c | `pg_terminate_backend` during COMMIT | EXTERNAL | Either outcome | **Rolled back**. Worker logged “outcome may be uncertain”, requeued, and the same process completed once. |
| E4 | Commit visible while worker is SIGSTOPped, then SIGKILL | ACTUAL | Success survives; redelivery terminal | Harness connection saw done + result + succeeded while `/proc` state was `T` and the delivery was unacked; after kill ready=1 (no ACK ever sent); restart ACKed without changing state. |
| E5a | API SIGKILL after broker confirm, mark-published statement left to the server | ACTUAL | Observe | Server completed the in-flight `UPDATE`: `published_at` durable, 1 attempt, no republication within 3.2 s of restart. The window did **not** open. |
| E5b | Same, plus termination of the in-flight statement | ACTUAL + EXTERNAL | Required: `published_at` NULL with the confirmed message queued, then duplicate publication | `published_at` NULL with 1 confirmed message in RabbitMQ; restarted API republished (2 messages, 2 attempts); worker committed once and ACKed both. |
| E6 | Two worker processes hold copies of one message | ACTUAL; duplicate publication SIMULATED | One completion | One snapshot showed both backends waiting on the item lock (B blocked by harness and A); 2 unacked deliveries; after release: one result, attempt 1, both ACKed, no requeues. |
| E10 | SIGTERM idle and in flight | ACTUAL | Observe | Idle: exit 0 in 2 ms. In flight (lock wait): exit 0 in 5 ms, no backend left, message ready again; item untouched; restart completed once. |
| E11a | `docker stop` PostgreSQL with a message arriving | EXTERNAL | Requeue without recorded failure | 11 session restarts in 10 s, intervals 1005–1010 ms (median 1005); worker stayed up; success observed from a separate connection 640 ms after invoking `docker start`; attempt 1. |
| E11b | `docker kill` PostgreSQL during COMMIT (worker paused) | EXTERNAL | Either outcome | **Rolled back** after crash recovery; on SIGCONT the worker logged an uncertain commit, requeued, and completed once. |
| E12 | Destroy isolated volume, migrate, restore snapshot | EXTERNAL; processes ACTUAL | See below | See below. |

Deterministic integration results (CI): processor failures are **SIMULATED**
with test-only processors. Three failures mark the job `failed` (attempt 3,
`finished`, `processing_failed`) with the item pending and no result; a later
job completes and owns the result while the failed job is unchanged. Two
injected failures followed by success through real RabbitMQ redeliveries end
`done` with attempt 3. 32 concurrent HTTP requests, after every pooled API
connection was observed waiting on the item lock, produced one 202 and 31
`409 processing_already_active` naming the same job, with one job and one
outbox row; requests for a done item all returned `work_item_already_done`.

### Snapshot recovery (E12)

The recovery point held B done, C accepted with its outbox published (message
in RabbitMQ), and A accepted with its outbox unpublished (broker stopped). After
the backup, A and C completed, and D was accepted and published but not
consumed. Then the isolated volume was destroyed and restored migration-first.

- **B** restored identically.
- **A** resumed: the restored unpublished outbox row was republished and the
  worker completed it — the #111 behavior holds with results.
- **C** is permanently blocked: accepted job, outbox `published_at` set, no
  message (it was consumed before the loss). A new request returns
  `409 processing_already_active`. This is ADR 0003's known missing-delivery
  limitation.
- **Stale message after sequence rewind (reproduced; distinct risk).** All
  three sequences returned to their backup values. D's message (job 195,
  item 183) remained in the independently durable broker. A new item E created
  after restore received item 183 and job 195; the experiment requires this
  collision and fails without it. With E's own outbox row still
  unpublished, the worker consumed D's stale message and **completed E**;
  E's result used E's title because the worker reads input from PostgreSQL.

Triggering conditions for the alias: (1) work accepted and published after the
recovery point; (2) its message survives in RabbitMQ while PostgreSQL is rolled
back; (3) new work reaches the identical `(job_id, work_item_id)` pair before the
stale message is consumed. By code inspection (not exercised): if only `job_id`
matched, the worker rejects the message as an identity mismatch; if the job did
not exist yet, it rejects it as unknown — dropping it, which is harmless for
lost work.

Implications: this is an extra, misattributed delivery, not a missing one. It
was benign here only because messages carry identities and processing is
deterministic over database state. It breaks provenance (E completed without its
own message ever being published), would count injected or real failures
against the wrong job, and would become unsafe if messages carried business
data, processing versions, or triggered external effects.

## Analysis

- Client-observed COMMIT failure is not evidence of rollback: E3a committed
  after the client died, while E3b/E3c/E11b rolled back. Inspecting job state
  on redelivery (not converting uncertainty into failure) handled every observed
  outcome. The commit gate widens a window that is normally short but non-zero.
- Duplicate publication from the outbox is real but conditional (E5a vs E5b);
  worker-side terminal-state inspection makes it harmless for this result.
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

No architectural decision changes in this issue. Candidate follow-ups:

- Restore procedure or redrive work for blocked accepted jobs (ADR 0003 already
  scopes this out).
- Prevent stale-message aliasing after restore: quarantine/purge queued messages
  for identities beyond the restored sequences, advance sequences past the
  pre-loss high-water mark, or add a non-reusable delivery identity checked by
  the worker. This needs its own decision.

## Cleanup

The harness is **kept** as opt-in scaffolding; CI vets it. Instrumentation
objects (`issue118` schema, `pageinspect`, gate triggers) exist only in the
isolated database and are removed by destroying it. Tear down the isolated lab
with the commands in the [local guide](../guides/local-work-items.md#isolated-crash-consistency-lab).
