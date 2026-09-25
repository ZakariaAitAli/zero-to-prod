package main

import (
	"context"
	"testing"
	"time"
)

func TestRecordProcessingFailurePostgresIntegration(
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

	const (
		errorCode   = "processing_failed"
		maxAttempts = 3
	)

	first, err := store.RecordProcessingFailure(
		ctx,
		jobID,
		workItemID,
		errorCode,
		maxAttempts,
	)
	if err != nil {
		t.Fatalf(
			"record first processing failure: %v",
			err,
		)
	}

	if first.Disposition != processingFailureRetryable {
		t.Fatalf(
			"expected first disposition %q, got %q",
			processingFailureRetryable,
			first.Disposition,
		)
	}

	if first.Job.State != "accepted" ||
		first.Job.AttemptCount != 1 ||
		first.Job.FinishedAt != nil {
		t.Fatalf(
			"unexpected first failure state: state=%q attempts=%d finished=%v",
			first.Job.State,
			first.Job.AttemptCount,
			first.Job.FinishedAt,
		)
	}

	second, err := store.RecordProcessingFailure(
		ctx,
		jobID,
		workItemID,
		errorCode,
		maxAttempts,
	)
	if err != nil {
		t.Fatalf(
			"record second processing failure: %v",
			err,
		)
	}

	if second.Disposition != processingFailureRetryable {
		t.Fatalf(
			"expected second disposition %q, got %q",
			processingFailureRetryable,
			second.Disposition,
		)
	}

	if second.Job.State != "accepted" ||
		second.Job.AttemptCount != 2 ||
		second.Job.FinishedAt != nil {
		t.Fatalf(
			"unexpected second failure state: state=%q attempts=%d finished=%v",
			second.Job.State,
			second.Job.AttemptCount,
			second.Job.FinishedAt,
		)
	}

	third, err := store.RecordProcessingFailure(
		ctx,
		jobID,
		workItemID,
		errorCode,
		maxAttempts,
	)
	if err != nil {
		t.Fatalf(
			"record third processing failure: %v",
			err,
		)
	}

	if third.Disposition != processingFailureTerminal {
		t.Fatalf(
			"expected third disposition %q, got %q",
			processingFailureTerminal,
			third.Disposition,
		)
	}

	if third.Job.State != "failed" ||
		third.Job.AttemptCount != 3 ||
		third.Job.FinishedAt == nil {
		t.Fatalf(
			"unexpected terminal failure state: state=%q attempts=%d finished=%v",
			third.Job.State,
			third.Job.AttemptCount,
			third.Job.FinishedAt,
		)
	}

	if third.Job.LastError == nil ||
		*third.Job.LastError != errorCode {
		t.Fatalf(
			"unexpected terminal error code: %#v",
			third.Job.LastError,
		)
	}

	redelivery, err := store.RecordProcessingFailure(
		ctx,
		jobID,
		workItemID,
		errorCode,
		maxAttempts,
	)
	if err != nil {
		t.Fatalf(
			"classify exhausted-job redelivery: %v",
			err,
		)
	}

	if redelivery.Disposition !=
		processingFailureAlreadyTerminal {
		t.Fatalf(
			"expected redelivery disposition %q, got %q",
			processingFailureAlreadyTerminal,
			redelivery.Disposition,
		)
	}

	if redelivery.Job.State != "failed" ||
		redelivery.Job.AttemptCount != 3 {
		t.Fatalf(
			"redelivery changed terminal state: state=%q attempts=%d",
			redelivery.Job.State,
			redelivery.Job.AttemptCount,
		)
	}
}
