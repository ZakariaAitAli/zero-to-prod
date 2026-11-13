package main

import (
	"context"
	"testing"
	"time"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/workitems"
)

// A wait inside the completion transaction must count toward job latency.
// CURRENT_TIMESTAMP is fixed at BEGIN and silently excludes this wait.
func TestCompletionTimestampIncludesTransactionLockWaitPostgresIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	blocker, err := fixture.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, workitems.ItemLockSQL, fixture.WorkItemID); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		completion processingCompletion
		err        error
	}
	finished := make(chan outcome, 1)
	go func() {
		completion, err := fixture.Store.CompleteProcessingJob(ctx, fixture.JobID, fixture.WorkItemID)
		finished <- outcome{completion, err}
	}()
	// The fixture is isolated and these integration tests are not parallel.
	for {
		var waiting bool
		if err := fixture.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case result := <-finished:
			t.Fatalf("completion did not wait for the item lock: %v", result.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	var whileBlocked time.Time
	if err := fixture.Pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&whileBlocked); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-finished:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.completion.Job.FinishedAt == nil || result.completion.Job.FinishedAt.Before(whileBlocked) {
			t.Fatal("finished_at predates the completion transaction's lock wait")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
