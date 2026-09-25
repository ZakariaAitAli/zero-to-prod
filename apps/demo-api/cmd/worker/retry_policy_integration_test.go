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
