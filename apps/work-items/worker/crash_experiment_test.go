//go:build crashexperiment

package main

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// recoverWithFreshWorker starts a new worker process and proves that the
// outstanding delivery was settled without requeue and the item is complete.
func (lab *experimentLab) recoverWithFreshWorker(t *testing.T, name string, workItemID, jobID int64) businessState {
	t.Helper()
	worker := startWorker(t, lab.config, lab.record, name, nil)
	waitForState(t, lab.record, lab.admin, workItemID, name+" completes item", "item done with exactly one result from the job",
		func(state businessState) bool { return isCompletedOnce(state, jobID) })
	waitForQueue(t, lab.config, lab.record, name+" settles delivery", queueCounts{Consumers: 1})
	stopGracefully(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, name+" consumer removed without requeue", queueCounts{})
	return checkState(t, lab.record, lab.admin, workItemID, "final state after recovery",
		"done; one result; succeeded job attempt_count=1", func(state businessState) bool { return isCompletedOnce(state, jobID) })
}

func TestIssue118E1CrashBeforeResultInsertion(t *testing.T) {
	lab := openExperimentLab(t, "e1-crash-before-result-insertion", "ACTUAL worker process crash (SIGKILL)",
		"Does a worker crash inside the completion transaction, before any write, leave the job redeliverable with no business effect?")
	item, job := lab.createAcceptedJob(t, "issue118 E1 crash before insert")
	holder := lab.holdSessionLock(t, itemLockKey, item)
	lab.publish(t, item, job)
	worker := startWorker(t, lab.config, lab.record, "e1-a", nil)

	backend := waitForBackend(t, lab.record, lab.admin, appName("e1-a"), "worker reaches completion transaction",
		"worker backend waits on the per-item advisory lock held by the harness",
		func(backend backendObservation) bool { return isItemLockWait(backend, holder) })
	lab.record.check("no write executed yet (no transaction ID assigned)", "", backend.XID, backend.XID == "")
	waitForQueue(t, lab.config, lab.record, "delivery held by worker", queueCounts{Unacked: 1, Consumers: 1})

	killProcess(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "broker requeues unacknowledged delivery", queueCounts{Ready: 1})
	orphans, _ := backends(context.Background(), lab.admin, appName("e1-a"))
	lab.record.observe("backends_after_client_death_before_lock_release", orphans)
	lab.releaseSessionLock(t, holder, itemLockKey, item)
	waitForNoBackend(t, lab.record, lab.admin, appName("e1-a"), "orphaned backend exits")
	checkState(t, lab.record, lab.admin, item, "durable state after crash", "pending; accepted; attempt_count=0; no result",
		func(state businessState) bool { return isUntouched(state, job) })

	lab.recoverWithFreshWorker(t, "e1-b", item, job)
}

func TestIssue118E2CrashAfterWritesBeforeCommit(t *testing.T) {
	lab := openExperimentLab(t, "e2-crash-after-writes-before-commit", "ACTUAL worker process crash (SIGKILL)",
		"After the result insert and Work Item update have executed, does a crash at the guarded job update roll every write back?")
	item, job := lab.createAcceptedJob(t, "issue118 E2 crash before commit")
	ctx := context.Background()

	// FOR NO KEY UPDATE conflicts with the guarded job UPDATE but not with the
	// FOR KEY SHARE lock taken by the result's foreign-key check.
	holder, err := pgx.Connect(ctx, withParams(lab.config.AdminURL, map[string]string{"application_name": "issue118-harness"}))
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close(ctx)
	var holderPID int32
	if err := holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	holderTx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holderTx.Exec(ctx, "SELECT id FROM public.processing_jobs WHERE id=$1 FOR NO KEY UPDATE", job); err != nil {
		t.Fatal(err)
	}
	lab.record.sync("hold job row lock", fmt.Sprintf("harness pid %d holds FOR NO KEY UPDATE on job %d", holderPID, job), nil)

	lab.publish(t, item, job)
	worker := startWorker(t, lab.config, lab.record, "e2-a", nil)
	backend := waitForBackend(t, lab.record, lab.admin, appName("e2-a"), "worker reaches guarded job update",
		"worker backend has a transaction ID and waits on the job row lock in the guarded processing_jobs UPDATE",
		func(backend backendObservation) bool {
			return backend.WaitEventType == "Lock" && strings.Contains(backend.Query, "UPDATE public.processing_jobs") &&
				backend.XID != "" && blockedBy(backend, holderPID)
		})
	writes, err := writesBy(ctx, lab.admin, backend.XID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"public.work_item_results": 1, "public.work_items": 1, "public.processing_jobs": 0}
	lab.record.check("preceding writes executed by the blocked transaction (heap tuples with its xmin)", want, writes,
		writes["public.work_item_results"] == 1 && writes["public.work_items"] == 1 && writes["public.processing_jobs"] == 0)

	killProcess(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "broker requeues unacknowledged delivery", queueCounts{Ready: 1})
	if err := holderTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	lab.record.sync("release job row lock", "harness transaction rolled back", nil)
	waitForNoBackend(t, lab.record, lab.admin, appName("e2-a"), "orphaned backend exits and aborts")
	checkState(t, lab.record, lab.admin, item, "durable state after crash", "pending; accepted; attempt_count=0; no result",
		func(state businessState) bool { return isUntouched(state, job) })
	lab.record.observe("tuples_physically_written_by_aborted_transaction", mustWrites(t, lab, backend.XID))

	lab.recoverWithFreshWorker(t, "e2-b", item, job)
}

func mustWrites(t *testing.T, lab *experimentLab, xid string) map[string]int {
	t.Helper()
	writes, err := writesBy(context.Background(), lab.admin, xid)
	if err != nil {
		t.Fatal(err)
	}
	return writes
}

type commitInterruption string

const (
	interruptKillClient          commitInterruption = "SIGKILL worker process during COMMIT"
	interruptKillClientWithCheck commitInterruption = "SIGKILL worker process during COMMIT with client_connection_check_interval=100ms"
	interruptTerminateBackend    commitInterruption = "pg_terminate_backend on the worker backend during COMMIT"
	commitOutcomeCommitted                          = "committed"
	commitOutcomeRolledBack                         = "rolled_back"
)

func classifyCommitOutcome(t *testing.T, lab *experimentLab, item, job int64) string {
	t.Helper()
	state := checkState(t, lab.record, lab.admin, item, "all-or-nothing state after interrupted COMMIT",
		"either untouched (rolled back) or completed once (committed)",
		func(state businessState) bool { return isUntouched(state, job) || isCompletedOnce(state, job) })
	outcome := commitOutcomeRolledBack
	if isCompletedOnce(state, job) {
		outcome = commitOutcomeCommitted
	}
	lab.record.observe("observed_commit_outcome", outcome)
	return outcome
}

func runCommitInterruption(t *testing.T, id string, interruption commitInterruption, extra map[string]string) {
	classification := "ACTUAL worker process crash (SIGKILL)"
	if interruption == interruptTerminateBackend {
		classification = "EXTERNAL database backend termination; worker process keeps running"
	}
	lab := openExperimentLab(t, id, classification,
		"When the worker loses its COMMIT, is the observed outcome all-or-nothing and does redelivery recover safely?")
	lab.record.observe("interruption", string(interruption))
	item, job := lab.createAcceptedJob(t, "issue118 "+id)
	lab.installCommitGate(t, item)
	gate := lab.holdSessionLock(t, commitGateLockKey, item)
	lab.publish(t, item, job)
	name := id + "-a"
	worker := startWorker(t, lab.config, lab.record, name, extra)
	backend := waitForBackend(t, lab.record, lab.admin, appName(name), "worker reaches COMMIT",
		"worker backend executes COMMIT and waits on the harness-held commit gate",
		func(backend backendObservation) bool { return isCommitGateWait(backend, gate) })
	writes := mustWrites(t, lab, backend.XID)
	lab.record.check("all success writes executed before COMMIT", "1 result, 1 item version, 1 job version", writes,
		writes["public.work_item_results"] == 1 && writes["public.work_items"] == 1 && writes["public.processing_jobs"] == 1)

	ctx := context.Background()
	switch interruption {
	case interruptKillClient, interruptKillClientWithCheck:
		killProcess(t, lab.record, worker)
		waitForQueue(t, lab.config, lab.record, "unacknowledged delivery requeued after client death", queueCounts{Ready: 1})
		gone, observation, elapsed := observeWithin(2*time.Second, func() (bool, any, error) {
			observed, err := backends(ctx, lab.admin, appName(name))
			return len(observed) == 0, observed, err
		})
		lab.record.observe("backend_exited_before_gate_release", map[string]any{
			"exited": gone, "window_ms": elapsed.Milliseconds(), "last_observation": observation})
		lab.releaseSessionLock(t, gate, commitGateLockKey, item)
		waitForNoBackend(t, lab.record, lab.admin, appName(name), "backend finished")
		outcome := classifyCommitOutcome(t, lab, item, job)
		final := lab.recoverWithFreshWorker(t, id+"-b", item, job)
		if outcome == commitOutcomeCommitted {
			lab.record.check("redelivery recognized committed success without reprocessing", 1,
				final.Jobs[0].AttemptCount, final.Jobs[0].AttemptCount == 1)
		}

	case interruptTerminateBackend:
		var terminated bool
		if err := lab.admin.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backend.PID).Scan(&terminated); err != nil || !terminated {
			t.Fatalf("terminate backend: %t %v", terminated, err)
		}
		waitUntil(t, lab.record, "worker observes uncertain commit", "worker log reports commit outcome may be uncertain",
			func() (bool, any, error) {
				lines := worker.linesContaining("outcome may be uncertain")
				return len(lines) > 0, len(lines), nil
			})
		waitUntil(t, lab.record, "terminated backend gone", fmt.Sprintf("backend pid %d absent", backend.PID), func() (bool, any, error) {
			observed, err := backends(ctx, lab.admin, appName(name))
			for _, current := range observed {
				if current.PID == backend.PID {
					return false, observed, err
				}
			}
			return true, observed, err
		})
		// The gate is still held, so any redelivered attempt cannot commit yet.
		outcome := classifyCommitOutcome(t, lab, item, job)
		lab.releaseSessionLock(t, gate, commitGateLockKey, item)
		waitForState(t, lab.record, lab.admin, item, "same worker process recovers", "item done once",
			func(state businessState) bool { return isCompletedOnce(state, job) })
		waitForQueue(t, lab.config, lab.record, "delivery settled", queueCounts{Consumers: 1})
		lab.record.check("worker process survived backend termination", true, worker.running(), worker.running())
		stopGracefully(t, lab.record, worker)
		waitForQueue(t, lab.config, lab.record, "consumer removed without requeue", queueCounts{})
		checkState(t, lab.record, lab.admin, item, "final state", "done once", func(state businessState) bool {
			return isCompletedOnce(state, job)
		})
		lab.record.observe("recovery_path", map[string]string{commitOutcomeRolledBack: "redelivery reprocessed",
			commitOutcomeCommitted: "redelivery acknowledged terminal success"}[outcome])
	}
}

func TestIssue118E3aKillClientDuringCommit(t *testing.T) {
	runCommitInterruption(t, "e3a-kill-client-during-commit", interruptKillClient, nil)
}

func TestIssue118E3bKillClientDuringCommitWithConnectionCheck(t *testing.T) {
	runCommitInterruption(t, "e3b-kill-client-during-commit-connection-check", interruptKillClientWithCheck,
		map[string]string{"client_connection_check_interval": "100"})
}

func TestIssue118E3cTerminateBackendDuringCommit(t *testing.T) {
	runCommitInterruption(t, "e3c-terminate-backend-during-commit", interruptTerminateBackend, nil)
}

func TestIssue118E4CrashAfterCommitBeforeAck(t *testing.T) {
	lab := openExperimentLab(t, "e4-crash-after-commit-before-ack", "ACTUAL worker process crash (SIGSTOP then SIGKILL)",
		"If success commits but the worker dies before RabbitMQ acknowledgement, does redelivery recognize terminal success?")
	item, job := lab.createAcceptedJob(t, "issue118 E4 crash before ack")
	lab.installCommitGate(t, item)
	gate := lab.holdSessionLock(t, commitGateLockKey, item)
	lab.publish(t, item, job)
	worker := startWorker(t, lab.config, lab.record, "e4-a", nil)
	backend := waitForBackend(t, lab.record, lab.admin, appName("e4-a"), "worker reaches COMMIT",
		"worker backend executes COMMIT and waits on the commit gate",
		func(backend backendObservation) bool { return isCommitGateWait(backend, gate) })
	lab.record.observe("writes_before_commit", mustWrites(t, lab, backend.XID))

	worker.signal(t, syscall.SIGSTOP)
	waitUntil(t, lab.record, "worker stopped", "/proc state T", func() (bool, any, error) {
		state := worker.kernelState()
		return state == "T", state, nil
	})
	lab.releaseSessionLock(t, gate, commitGateLockKey, item)
	committed := waitForState(t, lab.record, lab.admin, item, "success visible from another connection",
		"harness connection reads done item, one result, succeeded job", func(state businessState) bool {
			return isCompletedOnce(state, job)
		})
	lab.record.check("worker still stopped while success is visible", "T", worker.kernelState(), worker.kernelState() == "T")
	waitForQueue(t, lab.config, lab.record, "delivery still unacknowledged by stopped worker", queueCounts{Unacked: 1, Consumers: 1})
	lab.record.check("worker still stopped before kill", "T", worker.kernelState(), worker.kernelState() == "T")

	killProcess(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "broker requeues: no ACK was ever sent", queueCounts{Ready: 1})
	final := lab.recoverWithFreshWorker(t, "e4-b", item, job)
	lab.record.check("redelivery changed nothing", committed.String(), final.String(), committed.String() == final.String())
}

func TestIssue118E6ConcurrentDuplicateDelivery(t *testing.T) {
	lab := openExperimentLab(t, "e6-concurrent-duplicate-delivery", "ACTUAL concurrent worker processes; duplicate publication simulated by the harness",
		"When two worker processes hold copies of the same message concurrently, does exactly one business completion commit?")
	item, job := lab.createAcceptedJob(t, "issue118 E6 concurrent delivery")
	holder := lab.holdSessionLock(t, itemLockKey, item)
	lab.publish(t, item, job)
	lab.publish(t, item, job)
	workerA := startWorker(t, lab.config, lab.record, "e6-a", nil)
	waitForBackend(t, lab.record, lab.admin, appName("e6-a"), "worker A waits on item lock", "blocked by harness",
		func(backend backendObservation) bool { return isItemLockWait(backend, holder) })
	workerB := startWorker(t, lab.config, lab.record, "e6-b", nil)
	waitForBackend(t, lab.record, lab.admin, appName("e6-b"), "worker B waits on item lock", "blocked by harness",
		func(backend backendObservation) bool { return isItemLockWait(backend, holder) })

	// One snapshot proves both transactions are simultaneously in progress.
	var simultaneous int
	if err := lab.admin.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
 WHERE application_name IN ($1,$2) AND wait_event='advisory' AND $3 = ANY(pg_blocking_pids(pid))`,
		appName("e6-a"), appName("e6-b"), holder.pid).Scan(&simultaneous); err != nil {
		t.Fatal(err)
	}
	lab.record.check("both workers waiting at the same instant", 2, simultaneous, simultaneous == 2)
	waitForQueue(t, lab.config, lab.record, "each worker holds one copy", queueCounts{Unacked: 2, Consumers: 2})

	lab.releaseSessionLock(t, holder, itemLockKey, item)
	waitForState(t, lab.record, lab.admin, item, "one completion commits", "item done once",
		func(state businessState) bool { return isCompletedOnce(state, job) })
	waitForQueue(t, lab.config, lab.record, "both copies acknowledged", queueCounts{Consumers: 2})
	requeues := len(workerA.linesContaining("retrying")) + len(workerB.linesContaining("retrying"))
	lab.record.check("no handling error or requeue in either worker", 0, requeues, requeues == 0)
	stopGracefully(t, lab.record, workerA)
	stopGracefully(t, lab.record, workerB)
	waitForQueue(t, lab.config, lab.record, "consumers removed", queueCounts{})
	checkState(t, lab.record, lab.admin, item, "committed state", "done; one result; attempt_count=1",
		func(state businessState) bool { return isCompletedOnce(state, job) })
}

func TestIssue118E10GracefulShutdown(t *testing.T) {
	lab := openExperimentLab(t, "e10-graceful-shutdown", "ACTUAL worker process signal (SIGTERM)",
		"How does the worker behave on SIGTERM when idle and while a completion transaction is in flight?")
	idle := startWorker(t, lab.config, lab.record, "e10-idle", nil)
	idle.signal(t, syscall.SIGTERM)
	status, elapsed := idle.waitExit(t, 15*time.Second)
	lab.record.observe("idle_shutdown", map[string]any{"exit_status": status, "elapsed_ms": elapsed.Milliseconds()})
	waitForQueue(t, lab.config, lab.record, "idle consumer removed", queueCounts{})

	item, job := lab.createAcceptedJob(t, "issue118 E10 graceful in flight")
	holder := lab.holdSessionLock(t, itemLockKey, item)
	lab.publish(t, item, job)
	worker := startWorker(t, lab.config, lab.record, "e10-a", nil)
	waitForBackend(t, lab.record, lab.admin, appName("e10-a"), "worker in completion transaction", "waiting on item lock",
		func(backend backendObservation) bool { return isItemLockWait(backend, holder) })
	worker.signal(t, syscall.SIGTERM)
	status, elapsed = worker.waitExit(t, 15*time.Second)
	afterExit, _ := backends(context.Background(), lab.admin, appName("e10-a"))
	counts, _ := rabbitQueueCounts(lab.config)
	lab.record.observe("in_flight_shutdown", map[string]any{"exit_status": status, "elapsed_ms": elapsed.Milliseconds(),
		"backends_after_exit": afterExit, "queue_after_exit": counts})
	waitForQueue(t, lab.config, lab.record, "in-flight delivery retained by broker", queueCounts{Ready: 1})
	lab.releaseSessionLock(t, holder, itemLockKey, item)
	waitForNoBackend(t, lab.record, lab.admin, appName("e10-a"), "backend gone")
	checkState(t, lab.record, lab.admin, item, "durable state after shutdown", "pending; accepted; attempt_count=0; no result",
		func(state businessState) bool { return isUntouched(state, job) })
	lab.recoverWithFreshWorker(t, "e10-b", item, job)
}
