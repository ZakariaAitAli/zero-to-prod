//go:build crashexperiment

package main

import (
	"slices"
	"sort"
	"syscall"
	"testing"
	"time"
)

const outageMeasurementWindow = 10 * time.Second

func TestIssue118E11aPostgresOutageRetry(t *testing.T) {
	lab := openExperimentLab(t, "e11a-postgres-outage-retry", "EXTERNAL PostgreSQL outage (docker stop of the isolated container)",
		"How does the worker behave while PostgreSQL is unavailable, and does a delivery complete once it returns?")
	item, job := lab.createAcceptedJob(t, "issue118 E11 outage")
	worker := startWorker(t, lab.config, lab.record, "e11a", nil)
	lab.postgresAction(t, "stop")
	published := time.Since(worker.started)
	lab.publish(t, item, job)

	// Measurement window: sample the broker while PostgreSQL stays down.
	var samples []queueCounts
	measurementEnd := time.Now().Add(outageMeasurementWindow)
	for time.Now().Before(measurementEnd) {
		if counts, err := rabbitQueueCounts(lab.config); err == nil {
			samples = append(samples, counts)
		}
	}
	var retries []time.Duration
	for _, line := range worker.linesContaining("worker session ended; retrying") {
		if line.At > published {
			retries = append(retries, line.At)
		}
	}
	var intervals []int64
	for index := 1; index < len(retries); index++ {
		intervals = append(intervals, (retries[index] - retries[index-1]).Milliseconds())
	}
	sorted := slices.Clone(intervals)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	summary := map[string]any{"window_ms": outageMeasurementWindow.Milliseconds(), "requeue_session_restarts": len(retries),
		"intervals_ms": intervals, "inspection_unavailable_logs": len(worker.linesContaining("processing job inspection unavailable")),
		"queue_samples": len(samples), "worker_running": worker.running()}
	if len(sorted) > 0 {
		summary["interval_min_ms"], summary["interval_median_ms"], summary["interval_max_ms"] =
			sorted[0], sorted[len(sorted)/2], sorted[len(sorted)-1]
	}
	if len(samples) > 0 {
		summary["queue_first_sample"], summary["queue_last_sample"] = samples[0], samples[len(samples)-1]
	}
	lab.record.observe("outage_measurement", summary)
	lab.record.check("worker process survived the outage", true, worker.running(), worker.running())
	lab.record.check("delivery was retried during the outage", ">= 1 session restart", len(retries), len(retries) >= 1)

	// Observation latency: from just before docker start is invoked until a
	// separate harness connection reads committed success. It bounds recovery
	// from above (polling every 10 ms; read errors while PostgreSQL starts are
	// retried). finished_at is not used: CURRENT_TIMESTAMP is transaction start.
	observationStart := time.Now()
	lab.postgresStartWithoutHealthWait(t)
	waitForState(t, lab.record, lab.admin, item, "completion after PostgreSQL returns",
		"harness connection reads item done once", func(state businessState) bool { return isCompletedOnce(state, job) })
	lab.record.observe("success_observation_latency_from_docker_start_ms", time.Since(observationStart).Milliseconds())
	lab.record.observe("session_restarts_until_completion", len(worker.linesContaining("worker session ended; retrying")))
	lab.waitPostgresHealthy(t)
	waitForQueue(t, lab.config, lab.record, "delivery acknowledged", queueCounts{Consumers: 1})
	stopGracefully(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "consumer removed without requeue", queueCounts{})
	checkState(t, lab.record, lab.admin, item, "final state", "done; attempt_count=1 (outage recorded no processing failure)",
		func(state businessState) bool { return isCompletedOnce(state, job) })
}

func TestIssue118E11bPostgresCrashDuringCommit(t *testing.T) {
	lab := openExperimentLab(t, "e11b-postgres-crash-during-commit", "EXTERNAL PostgreSQL crash (docker kill) during COMMIT; worker paused with SIGSTOP",
		"If PostgreSQL crashes while the worker's COMMIT is in progress, what is the durable outcome and does the worker recover?")
	item, job := lab.createAcceptedJob(t, "issue118 E11 crash during commit")
	lab.installCommitGate(t, item)
	gate := lab.holdSessionLock(t, commitGateLockKey, item)
	lab.publish(t, item, job)
	worker := startWorker(t, lab.config, lab.record, "e11b", nil)
	backend := waitForBackend(t, lab.record, lab.admin, appName("e11b"), "worker reaches COMMIT", "waiting on commit gate",
		func(backend backendObservation) bool { return isCommitGateWait(backend, gate) })
	lab.record.observe("writes_before_commit", mustWrites(t, lab, backend.XID))

	// Pause the worker so it cannot act on the crash before state is inspected.
	worker.signal(t, syscall.SIGSTOP)
	waitUntil(t, lab.record, "worker stopped", "/proc state T", func() (bool, any, error) {
		return worker.kernelState() == "T", worker.kernelState(), nil
	})
	lab.postgresAction(t, "kill")
	lab.postgresAction(t, "start")
	outcome := classifyCommitOutcome(t, lab, item, job)

	worker.signal(t, syscall.SIGCONT)
	waitForState(t, lab.record, lab.admin, item, "worker recovers", "item done once",
		func(state businessState) bool { return isCompletedOnce(state, job) })
	waitForQueue(t, lab.config, lab.record, "delivery acknowledged", queueCounts{Consumers: 1})
	lab.record.check("worker process survived the database crash", true, worker.running(), worker.running())
	lab.record.observe("worker_uncertain_commit_logs", len(worker.linesContaining("outcome may be uncertain")))
	stopGracefully(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "consumer removed without requeue", queueCounts{})
	checkState(t, lab.record, lab.admin, item, "final state", "done once", func(state businessState) bool {
		return isCompletedOnce(state, job)
	})
	lab.record.observe("observed_commit_outcome", outcome)
}
