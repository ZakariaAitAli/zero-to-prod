package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	errProcessingJobNotFound = errors.New("processing job not found")
	errProcessingJobMismatch = errors.New("processing job work item mismatch")
	errProcessingJobConflict = errors.New("processing job remained accepted after guarded completion")
)

type completionDisposition string

const (
	completionApplied         completionDisposition = "completed_now"
	completionAlreadyTerminal completionDisposition = "already_terminal"
)

type workerProcessingJob struct {
	ID           int64
	WorkItemID   int64
	State        string
	AttemptCount int
	LastError    *string
	FinishedAt   *time.Time
}

type processingCompletion struct {
	Disposition completionDisposition
	Job         workerProcessingJob
}

type workerQueryer interface {
	QueryRow(
		context.Context,
		string,
		...any,
	) pgx.Row
}

type workerStore struct {
	db workerQueryer
}

func scanWorkerProcessingJob(
	row pgx.Row,
) (workerProcessingJob, error) {
	var job workerProcessingJob

	err := row.Scan(
		&job.ID,
		&job.WorkItemID,
		&job.State,
		&job.AttemptCount,
		&job.LastError,
		&job.FinishedAt,
	)
	if err != nil {
		return workerProcessingJob{}, err
	}

	return job, nil
}

func (store *workerStore) CompleteProcessingJob(
	ctx context.Context,
	jobID int64,
	workItemID int64,
) (processingCompletion, error) {
	const completeQuery = `
		UPDATE public.processing_jobs
		SET
			state = 'succeeded',
			attempt_count = attempt_count + 1,
			last_error_code = NULL,
			finished_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND work_item_id = $2
		  AND state = 'accepted'
		RETURNING
			id,
			work_item_id,
			state,
			attempt_count,
			last_error_code,
			finished_at
	`

	job, err := scanWorkerProcessingJob(
		store.db.QueryRow(
			ctx,
			completeQuery,
			jobID,
			workItemID,
		),
	)
	if err == nil {
		return processingCompletion{
			Disposition: completionApplied,
			Job:         job,
		}, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return processingCompletion{}, fmt.Errorf(
			"complete accepted processing job: %w",
			err,
		)
	}

	const lookupQuery = `
		SELECT
			id,
			work_item_id,
			state,
			attempt_count,
			last_error_code,
			finished_at
		FROM public.processing_jobs
		WHERE id = $1
	`

	job, err = scanWorkerProcessingJob(
		store.db.QueryRow(
			ctx,
			lookupQuery,
			jobID,
		),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return processingCompletion{}, fmt.Errorf(
			"%w: id=%d",
			errProcessingJobNotFound,
			jobID,
		)
	}
	if err != nil {
		return processingCompletion{}, fmt.Errorf(
			"inspect processing job after guarded completion: %w",
			err,
		)
	}

	if job.WorkItemID != workItemID {
		return processingCompletion{}, fmt.Errorf(
			"%w: job_id=%d expected_work_item_id=%d actual_work_item_id=%d",
			errProcessingJobMismatch,
			jobID,
			workItemID,
			job.WorkItemID,
		)
	}

	switch job.State {
	case "succeeded", "failed":
		return processingCompletion{
			Disposition: completionAlreadyTerminal,
			Job:         job,
		}, nil

	case "accepted":
		return processingCompletion{}, fmt.Errorf(
			"%w: job_id=%d work_item_id=%d",
			errProcessingJobConflict,
			jobID,
			workItemID,
		)

	default:
		return processingCompletion{}, fmt.Errorf(
			"%w: job_id=%d unexpected_state=%q",
			errProcessingJobConflict,
			jobID,
			job.State,
		)
	}
}
