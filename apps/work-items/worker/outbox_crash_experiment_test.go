//go:build crashexperiment

package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type outboxRow struct {
	Published bool `json:"published"`
	Attempts  int  `json:"publish_attempts"`
}

func readOutbox(t *testing.T, lab *experimentLab, job int64) outboxRow {
	t.Helper()
	var row outboxRow
	if err := lab.admin.QueryRow(context.Background(),
		"SELECT published_at IS NOT NULL, publish_attempts FROM public.outbox_messages WHERE processing_job_id=$1", job).
		Scan(&row.Published, &row.Attempts); err != nil {
		t.Fatal(err)
	}
	return row
}

// runOutboxPublisherCrash kills the real API process after RabbitMQ confirmed
// a publication but while its mark-published UPDATE is blocked.
func runOutboxPublisherCrash(t *testing.T, id string, terminateStatement bool) {
	classification := "ACTUAL API process crash (SIGKILL) after broker confirm; in-flight mark-published statement left to the server"
	if terminateStatement {
		classification = "ACTUAL API process crash (SIGKILL) after broker confirm, plus EXTERNAL termination of the in-flight mark-published statement"
	}
	lab := openExperimentLab(t, id, classification,
		"What happens when the outbox publisher dies after the broker confirmed a message but before published_at is durable?")
	var unpublished int
	if err := lab.admin.QueryRow(context.Background(), "SELECT count(*) FROM public.outbox_messages WHERE published_at IS NULL").
		Scan(&unpublished); err != nil {
		t.Fatal(err)
	}
	lab.record.check("precondition: no other unpublished outbox rows", 0, unpublished, unpublished == 0)
	api, baseURL := startAPI(t, lab.config, lab.record, id+"-api-a")
	status, body := httpJSON(t, http.MethodPost, baseURL+"/items", map[string]string{"title": "issue118 " + id})
	if status != http.StatusCreated {
		t.Fatalf("create item: %d %v", status, body)
	}
	item := jsonID(t, body, "id")

	ctx := context.Background()
	name := fmt.Sprintf("issue118_outbox_gate_%d", item)
	if _, err := lab.admin.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION issue118.outbox_gate() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(%d, hashint8((OLD.payload->>'work_item_id')::bigint)); RETURN NEW; END $$;
 CREATE TRIGGER %s BEFORE UPDATE ON public.outbox_messages FOR EACH ROW
 WHEN (OLD.published_at IS NULL AND NEW.published_at IS NOT NULL AND (OLD.payload->>'work_item_id')::bigint = %d)
 EXECUTE FUNCTION issue118.outbox_gate();`, outboxGateLockKey, name, item)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lab.admin.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON public.outbox_messages", name))
	})
	lab.record.observe("instrumentation", "test-only BEFORE UPDATE trigger blocks only the published_at transition on a harness-held advisory lock")
	gate := lab.holdSessionLock(t, outboxGateLockKey, item)

	status, body = httpJSON(t, http.MethodPost, fmt.Sprintf("%s/items/%d/process", baseURL, item), nil)
	if status != http.StatusAccepted {
		t.Fatalf("accept processing: %d %v", status, body)
	}
	job := jsonID(t, body, "id")
	lab.record.observe("work_item_id", item)
	lab.record.observe("job_id", job)
	backend := waitForBackend(t, lab.record, lab.admin, appName(id+"-api-a"), "publisher blocked marking published",
		"API backend runs the published_at UPDATE and waits on the outbox gate",
		func(backend backendObservation) bool {
			return backend.WaitEventType == "Lock" && backend.WaitEvent == "advisory" &&
				strings.Contains(backend.Query, "published_at = CURRENT_TIMESTAMP") && blockedBy(backend, gate.pid)
		})
	waitForQueue(t, lab.config, lab.record, "broker already holds the confirmed message", queueCounts{Ready: 1})

	killProcess(t, lab.record, api)
	if terminateStatement {
		var terminated bool
		if err := lab.admin.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backend.PID).Scan(&terminated); err != nil || !terminated {
			t.Fatalf("terminate backend: %t %v", terminated, err)
		}
	}
	lab.releaseSessionLock(t, gate, outboxGateLockKey, item)
	waitForNoBackend(t, lab.record, lab.admin, appName(id+"-api-a"), "API backends gone")
	afterCrash := readOutbox(t, lab, job)
	lab.record.observe("outbox_after_crash", afterCrash)
	lab.record.check("publish attempt was recorded before the crash", ">= 1", afterCrash.Attempts, afterCrash.Attempts >= 1)
	if terminateStatement {
		// E5b claims the crash window: the broker holds a confirmed message
		// while published_at is not durable.
		lab.record.check("crash window open: published_at remains NULL", false, afterCrash.Published, !afterCrash.Published)
		counts := mustQueueCounts(t, lab)
		lab.record.check("crash window open: confirmed message in broker", queueCounts{Ready: 1}, counts, counts == queueCounts{Ready: 1})
	}

	restarted, _ := startAPI(t, lab.config, lab.record, id+"-api-b")
	// E5a is observational: either outcome of the server-completed statement
	// is recorded. E5b reached the branch below only with published_at NULL.
	if afterCrash.Published {
		republished, observation, window := observeWithin(3*time.Second, func() (bool, any, error) {
			counts, err := rabbitQueueCounts(lab.config)
			return counts.Ready > 1, counts, err
		})
		lab.record.observe("republish_after_restart", map[string]any{"republished": republished, "window_ms": window.Milliseconds(),
			"last_queue": observation})
		lab.record.check("durable mark prevents republication", false, republished, !republished)
	} else {
		waitForQueue(t, lab.config, lab.record, "restarted publisher republishes the unmarked row", queueCounts{Ready: 2})
		marked := waitUntil(t, lab.record, "restarted publisher marks row", "published_at set", func() (bool, any, error) {
			row := readOutbox(t, lab, job)
			return row.Published, row, nil
		})
		lab.record.observe("duplicate_publication", map[string]any{"queued_copies": 2, "outbox_after_restart": marked})
	}
	if terminateStatement {
		final := readOutbox(t, lab, job)
		lab.record.check("duplicate publication after restart: second publish attempt recorded", 2, final.Attempts, final.Attempts == 2 && final.Published)
	}
	stopGracefully(t, lab.record, restarted)
	lab.record.observe("outbox_final", readOutbox(t, lab, job))

	worker := startWorker(t, lab.config, lab.record, id+"-worker", nil)
	waitForState(t, lab.record, lab.admin, item, "worker completes", "item done once",
		func(state businessState) bool { return isCompletedOnce(state, job) })
	waitForQueue(t, lab.config, lab.record, "every copy acknowledged", queueCounts{Consumers: 1})
	stopGracefully(t, lab.record, worker)
	waitForQueue(t, lab.config, lab.record, "consumer removed without requeue", queueCounts{})
	checkState(t, lab.record, lab.admin, item, "final state", "done; one result; attempt_count=1",
		func(state businessState) bool { return isCompletedOnce(state, job) })
}

func mustQueueCounts(t *testing.T, lab *experimentLab) queueCounts {
	t.Helper()
	counts, err := rabbitQueueCounts(lab.config)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// E5a is observational: it records whether the server completes the killed
// client's mark-published statement and whether republication follows.
func TestIssue118E5aOutboxPublisherCrashStatementCompletes(t *testing.T) {
	runOutboxPublisherCrash(t, "e5a-outbox-crash-statement-completes", false)
}

func TestIssue118E5bOutboxPublisherCrashWindowReproduced(t *testing.T) {
	runOutboxPublisherCrash(t, "e5b-outbox-crash-window-duplicate", true)
}
