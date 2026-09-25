package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationFailingProcessor struct {
	err   error
	calls int
}

func (processor *integrationFailingProcessor) Process(
	_ context.Context,
	_ workerMessage,
) error {
	processor.calls++
	return processor.err
}

func TestHandleWorkerMessageRetryExhaustionPostgresIntegration(
	t *testing.T,
) {
	databaseURL := os.Getenv("WORKER_DATABASE_URL")
	jobIDRaw := os.Getenv("WORKER_FAILURE_TEST_JOB_ID")
	workItemIDRaw := os.Getenv(
		"WORKER_FAILURE_TEST_WORK_ITEM_ID",
	)

	if databaseURL == "" ||
		jobIDRaw == "" ||
		workItemIDRaw == "" {
		t.Skip(
			"WORKER_DATABASE_URL, WORKER_FAILURE_TEST_JOB_ID, and WORKER_FAILURE_TEST_WORK_ITEM_ID are required",
		)
	}

	jobID, err := strconv.ParseInt(
		jobIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf(
			"parse retry-policy job id: %v",
			err,
		)
	}

	workItemID, err := strconv.ParseInt(
		workItemIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf(
			"parse retry-policy work item id: %v",
			err,
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"create worker PostgreSQL pool: %v",
			err,
		)
	}
	defer pool.Close()

	store := &workerStore{
		db: pool,
	}

	processingErr := errors.New(
		"synthetic representative processing failure",
	)

	processor := &integrationFailingProcessor{
		err: processingErr,
	}

	payload := []byte(
		fmt.Sprintf(
			`{"type":"work_item.process","version":1,"job_id":%d,"work_item_id":%d}`,
			jobID,
			workItemID,
		),
	)

	firstSettlement, firstErr :=
		handleWorkerMessageWithProcessor(
			ctx,
			store,
			processor,
			payload,
		)

	if firstSettlement != settlementNackRequeue {
		t.Fatalf(
			"first failure: expected %q, got %q",
			settlementNackRequeue,
			firstSettlement,
		)
	}

	if !errors.Is(firstErr, processingErr) {
		t.Fatalf(
			"first failure lost processing error: %v",
			firstErr,
		)
	}

	firstJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect first durable failure: %v",
			err,
		)
	}

	if firstJob.State != "accepted" ||
		firstJob.AttemptCount != 1 ||
		firstJob.FinishedAt != nil {
		t.Fatalf(
			"unexpected first durable state: state=%q attempts=%d finished=%v",
			firstJob.State,
			firstJob.AttemptCount,
			firstJob.FinishedAt,
		)
	}

	if firstJob.LastError == nil ||
		*firstJob.LastError != workerProcessingFailureCode {
		t.Fatalf(
			"unexpected first durable error code: %#v",
			firstJob.LastError,
		)
	}

	secondSettlement, secondErr :=
		handleWorkerMessageWithProcessor(
			ctx,
			store,
			processor,
			payload,
		)

	if secondSettlement != settlementNackRequeue {
		t.Fatalf(
			"second failure: expected %q, got %q",
			settlementNackRequeue,
			secondSettlement,
		)
	}

	if !errors.Is(secondErr, processingErr) {
		t.Fatalf(
			"second failure lost processing error: %v",
			secondErr,
		)
	}

	secondJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect second durable failure: %v",
			err,
		)
	}

	if secondJob.State != "accepted" ||
		secondJob.AttemptCount != 2 ||
		secondJob.FinishedAt != nil {
		t.Fatalf(
			"unexpected second durable state: state=%q attempts=%d finished=%v",
			secondJob.State,
			secondJob.AttemptCount,
			secondJob.FinishedAt,
		)
	}

	thirdSettlement, thirdErr :=
		handleWorkerMessageWithProcessor(
			ctx,
			store,
			processor,
			payload,
		)

	if thirdErr != nil {
		t.Fatalf(
			"terminal durable failure returned handling error: %v",
			thirdErr,
		)
	}

	if thirdSettlement != settlementAck {
		t.Fatalf(
			"third failure: expected terminal %q, got %q",
			settlementAck,
			thirdSettlement,
		)
	}

	thirdJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect terminal durable failure: %v",
			err,
		)
	}

	if thirdJob.State != "failed" ||
		thirdJob.AttemptCount != workerProcessingMaxAttempts ||
		thirdJob.FinishedAt == nil {
		t.Fatalf(
			"unexpected terminal durable state: state=%q attempts=%d finished=%v",
			thirdJob.State,
			thirdJob.AttemptCount,
			thirdJob.FinishedAt,
		)
	}

	if thirdJob.LastError == nil ||
		*thirdJob.LastError != workerProcessingFailureCode {
		t.Fatalf(
			"unexpected terminal durable error code: %#v",
			thirdJob.LastError,
		)
	}

	if processor.calls != workerProcessingMaxAttempts {
		t.Fatalf(
			"expected exactly %d processing attempts, got %d",
			workerProcessingMaxAttempts,
			processor.calls,
		)
	}

	redeliverySettlement, redeliveryErr :=
		handleWorkerMessageWithProcessor(
			ctx,
			store,
			processor,
			payload,
		)

	if redeliveryErr != nil {
		t.Fatalf(
			"terminal redelivery returned error: %v",
			redeliveryErr,
		)
	}

	if redeliverySettlement != settlementAck {
		t.Fatalf(
			"terminal redelivery: expected %q, got %q",
			settlementAck,
			redeliverySettlement,
		)
	}

	if processor.calls != workerProcessingMaxAttempts {
		t.Fatalf(
			"terminal redelivery reran processor: calls=%d",
			processor.calls,
		)
	}

	finalJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect final terminal job: %v",
			err,
		)
	}

	if finalJob.State != "failed" ||
		finalJob.AttemptCount != workerProcessingMaxAttempts {
		t.Fatalf(
			"terminal redelivery changed durable state: state=%q attempts=%d",
			finalJob.State,
			finalJob.AttemptCount,
		)
	}
}

type integrationFailOnceProcessor struct {
	err   error
	calls int
}

func (processor *integrationFailOnceProcessor) Process(
	_ context.Context,
	_ workerMessage,
) error {
	processor.calls++

	if processor.calls == 1 {
		return processor.err
	}

	return nil
}

func TestHandleWorkerMessageTransientFailureEventuallySucceedsPostgresIntegration(
	t *testing.T,
) {
	databaseURL := os.Getenv(
		"WORKER_DATABASE_URL",
	)
	jobIDRaw := os.Getenv(
		"WORKER_TRANSIENT_TEST_JOB_ID",
	)
	workItemIDRaw := os.Getenv(
		"WORKER_TRANSIENT_TEST_WORK_ITEM_ID",
	)

	if databaseURL == "" ||
		jobIDRaw == "" ||
		workItemIDRaw == "" {
		t.Skip(
			"WORKER_DATABASE_URL, WORKER_TRANSIENT_TEST_JOB_ID, and WORKER_TRANSIENT_TEST_WORK_ITEM_ID are required",
		)
	}

	jobID, err := strconv.ParseInt(
		jobIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf(
			"parse transient-retry job id: %v",
			err,
		)
	}

	workItemID, err := strconv.ParseInt(
		workItemIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf(
			"parse transient-retry Work Item id: %v",
			err,
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"create worker PostgreSQL pool: %v",
			err,
		)
	}
	defer pool.Close()

	store := &workerStore{
		db: pool,
	}

	processingErr := errors.New(
		"synthetic transient representative processing failure",
	)

	processor := &integrationFailOnceProcessor{
		err: processingErr,
	}

	payload := []byte(
		fmt.Sprintf(
			`{"type":"work_item.process","version":1,"job_id":%d,"work_item_id":%d}`,
			jobID,
			workItemID,
		),
	)

	firstSettlement, firstErr :=
		handleWorkerMessageWithProcessor(
			ctx,
			store,
			processor,
			payload,
		)

	if firstSettlement != settlementNackRequeue {
		t.Fatalf(
			"transient failure: expected %q, got %q",
			settlementNackRequeue,
			firstSettlement,
		)
	}

	if !errors.Is(firstErr, processingErr) {
		t.Fatalf(
			"transient failure lost processing error: %v",
			firstErr,
		)
	}

	firstJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect first transient attempt: %v",
			err,
		)
	}

	if firstJob.State != "accepted" {
		t.Fatalf(
			"first failure unexpectedly changed state to %q",
			firstJob.State,
		)
	}

	if firstJob.AttemptCount != 1 {
		t.Fatalf(
			"expected attempt_count=1 after first failure, got %d",
			firstJob.AttemptCount,
		)
	}

	if firstJob.FinishedAt != nil {
		t.Fatal(
			"retryable failure unexpectedly set finished_at",
		)
	}

	if firstJob.LastError == nil ||
		*firstJob.LastError != workerProcessingFailureCode {
		t.Fatalf(
			"unexpected durable retry error code: %#v",
			firstJob.LastError,
		)
	}

	secondSettlement, secondErr :=
		handleWorkerMessageWithProcessor(
			ctx,
			store,
			processor,
			payload,
		)

	if secondErr != nil {
		t.Fatalf(
			"successful retry returned error: %v",
			secondErr,
		)
	}

	if secondSettlement != settlementAck {
		t.Fatalf(
			"successful retry: expected %q, got %q",
			settlementAck,
			secondSettlement,
		)
	}

	finalJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect successful retry state: %v",
			err,
		)
	}

	if finalJob.State != "succeeded" {
		t.Fatalf(
			"expected eventual success, got state %q",
			finalJob.State,
		)
	}

	if finalJob.AttemptCount != 2 {
		t.Fatalf(
			"expected two durable processing attempts, got %d",
			finalJob.AttemptCount,
		)
	}

	if finalJob.LastError != nil {
		t.Fatalf(
			"successful retry did not clear last_error_code: %#v",
			finalJob.LastError,
		)
	}

	if finalJob.FinishedAt == nil {
		t.Fatal(
			"successful retry did not set finished_at",
		)
	}

	if processor.calls != 2 {
		t.Fatalf(
			"expected processor to run exactly twice, got %d",
			processor.calls,
		)
	}

	var workItemStatus string

	if err := pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM public.work_items
			WHERE id = $1
		`,
		workItemID,
	).Scan(
		&workItemStatus,
	); err != nil {
		t.Fatalf(
			"inspect Work Item status after retry success: %v",
			err,
		)
	}

	if workItemStatus != "pending" {
		t.Fatalf(
			"worker retry changed Work Item business status to %q",
			workItemStatus,
		)
	}

	t.Logf(
		"job_id=%d work_item_id=%d processor_calls=%d state=%s attempts=%d work_item_status=%s",
		jobID,
		workItemID,
		processor.calls,
		finalJob.State,
		finalJob.AttemptCount,
		workItemStatus,
	)
}
