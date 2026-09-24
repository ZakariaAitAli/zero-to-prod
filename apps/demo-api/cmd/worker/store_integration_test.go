package main

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompleteProcessingJobPostgresIntegration(
	t *testing.T,
) {
	databaseURL := os.Getenv("WORKER_DATABASE_URL")
	jobIDRaw := os.Getenv("WORKER_TEST_JOB_ID")
	workItemIDRaw := os.Getenv("WORKER_TEST_WORK_ITEM_ID")

	if databaseURL == "" ||
		jobIDRaw == "" ||
		workItemIDRaw == "" {
		t.Skip(
			"WORKER_DATABASE_URL, WORKER_TEST_JOB_ID, and WORKER_TEST_WORK_ITEM_ID are required",
		)
	}

	jobID, err := strconv.ParseInt(jobIDRaw, 10, 64)
	if err != nil {
		t.Fatalf("parse worker test job id: %v", err)
	}

	workItemID, err := strconv.ParseInt(
		workItemIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf("parse worker test work item id: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create worker PostgreSQL pool: %v", err)
	}
	defer pool.Close()

	store := &workerStore{db: pool}

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
