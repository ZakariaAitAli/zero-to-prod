package main

import (
	"context"
	"testing"
	"time"
)

func TestCompleteProcessingJobPostgresIntegration(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	fixture := requireWorkerPostgresIntegrationFixture(
		t,
		ctx,
	)

	store := fixture.Store
	jobID := fixture.JobID
	workItemID := fixture.WorkItemID

	first, err := store.CompleteProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf("complete accepted job: %v", err)
	}

	if first.Disposition != completionApplied {
		t.Fatalf(
			"expected first completion disposition %q, got %q",
			completionApplied,
			first.Disposition,
		)
	}

	if first.Job.State != "succeeded" {
		t.Fatalf(
			"expected first completion state succeeded, got %q",
			first.Job.State,
		)
	}

	if first.Job.AttemptCount != 1 {
		t.Fatalf(
			"expected first completion attempt_count=1, got %d",
			first.Job.AttemptCount,
		)
	}

	second, err := store.CompleteProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf("classify duplicate completion: %v", err)
	}

	if second.Disposition != completionAlreadyTerminal {
		t.Fatalf(
			"expected duplicate disposition %q, got %q",
			completionAlreadyTerminal,
			second.Disposition,
		)
	}

	if second.Job.State != "succeeded" {
		t.Fatalf(
			"expected duplicate state succeeded, got %q",
			second.Job.State,
		)
	}

	if second.Job.AttemptCount != 1 {
		t.Fatalf(
			"duplicate delivery changed attempt_count: %d",
			second.Job.AttemptCount,
		)
	}
}
