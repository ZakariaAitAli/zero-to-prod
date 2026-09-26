package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type workerStubRow struct {
	scan func(...any) error
}

func (row workerStubRow) Scan(dest ...any) error {
	return row.scan(dest...)
}

type workerQueryCall struct {
	query string
	args  []any
}

type workerStubDB struct {
	rows  []pgx.Row
	calls []workerQueryCall
}

func (db *workerStubDB) QueryRow(
	_ context.Context,
	query string,
	args ...any,
) pgx.Row {
	db.calls = append(db.calls, workerQueryCall{
		query: query,
		args:  append([]any(nil), args...),
	})

	if len(db.rows) == 0 {
		return workerStubRow{
			scan: func(...any) error {
				return errors.New("unexpected QueryRow call")
			},
		}
	}

	row := db.rows[0]
	db.rows = db.rows[1:]

	return row
}

func workerJobRow(
	job workerProcessingJob,
) pgx.Row {
	return workerStubRow{
		scan: func(dest ...any) error {
			if len(dest) != 6 {
				return fmt.Errorf(
					"expected 6 scan destinations, got %d",
					len(dest),
				)
			}

			*(dest[0].(*int64)) = job.ID
			*(dest[1].(*int64)) = job.WorkItemID
			*(dest[2].(*string)) = job.State
			*(dest[3].(*int)) = job.AttemptCount
			*(dest[4].(**string)) = job.LastError
			*(dest[5].(**time.Time)) = job.FinishedAt

			return nil
		},
	}
}

func workerErrorRow(err error) pgx.Row {
	return workerStubRow{
		scan: func(...any) error {
			return err
		},
	}
}

func TestCompleteProcessingJobAppliesAcceptedJobOnce(
	t *testing.T,
) {
	finishedAt := time.Date(
		2026,
		time.September,
		24,
		23,
		30,
		0,
		0,
		time.UTC,
	)

	db := &workerStubDB{
		rows: []pgx.Row{
			workerJobRow(workerProcessingJob{
				ID:           101,
				WorkItemID:   201,
				State:        "succeeded",
				AttemptCount: 1,
				FinishedAt:   &finishedAt,
			}),
		},
	}

	store := &workerStore{db: db}

	result, err := store.CompleteProcessingJob(
		context.Background(),
		101,
		201,
	)
	if err != nil {
		t.Fatalf("complete processing job: %v", err)
	}

	if result.Disposition != completionApplied {
		t.Fatalf(
			"expected %q, got %q",
			completionApplied,
			result.Disposition,
		)
	}

	if result.Job.State != "succeeded" {
		t.Fatalf(
			"expected succeeded state, got %q",
			result.Job.State,
		)
	}

	if result.Job.AttemptCount != 1 {
		t.Fatalf(
			"expected attempt_count=1, got %d",
			result.Job.AttemptCount,
		)
	}

	if len(db.calls) != 1 {
		t.Fatalf(
			"expected one database query, got %d",
			len(db.calls),
		)
	}

	if !strings.Contains(
		db.calls[0].query,
		"AND state = 'accepted'",
	) {
		t.Fatal("completion update is missing accepted-state guard")
	}
}

func TestCompleteProcessingJobClassifiesTerminalRedelivery(
	t *testing.T,
) {
	finishedAt := time.Now().UTC()

	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(pgx.ErrNoRows),
			workerJobRow(workerProcessingJob{
				ID:           301,
				WorkItemID:   401,
				State:        "succeeded",
				AttemptCount: 1,
				FinishedAt:   &finishedAt,
			}),
		},
	}

	store := &workerStore{db: db}

	result, err := store.CompleteProcessingJob(
		context.Background(),
		301,
		401,
	)
	if err != nil {
		t.Fatalf("classify terminal redelivery: %v", err)
	}

	if result.Disposition != completionAlreadyTerminal {
		t.Fatalf(
			"expected %q, got %q",
			completionAlreadyTerminal,
			result.Disposition,
		)
	}

	if result.Job.AttemptCount != 1 {
		t.Fatalf(
			"duplicate delivery changed attempt_count: %d",
			result.Job.AttemptCount,
		)
	}

	if len(db.calls) != 2 {
		t.Fatalf(
			"expected guarded update plus lookup, got %d queries",
			len(db.calls),
		)
	}
}

func TestCompleteProcessingJobDistinguishesUnknownJob(
	t *testing.T,
) {
	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(pgx.ErrNoRows),
			workerErrorRow(pgx.ErrNoRows),
		},
	}

	store := &workerStore{db: db}

	_, err := store.CompleteProcessingJob(
		context.Background(),
		501,
		601,
	)

	if !errors.Is(err, errProcessingJobNotFound) {
		t.Fatalf(
			"expected errProcessingJobNotFound, got %v",
			err,
		)
	}
}

func TestCompleteProcessingJobDistinguishesWorkItemMismatch(
	t *testing.T,
) {
	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(pgx.ErrNoRows),
			workerJobRow(workerProcessingJob{
				ID:           701,
				WorkItemID:   702,
				State:        "accepted",
				AttemptCount: 0,
			}),
		},
	}

	store := &workerStore{db: db}

	_, err := store.CompleteProcessingJob(
		context.Background(),
		701,
		999,
	)

	if !errors.Is(err, errProcessingJobMismatch) {
		t.Fatalf(
			"expected errProcessingJobMismatch, got %v",
			err,
		)
	}
}

func TestCompleteProcessingJobRejectsUnexpectedAcceptedFallback(
	t *testing.T,
) {
	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(pgx.ErrNoRows),
			workerJobRow(workerProcessingJob{
				ID:           801,
				WorkItemID:   901,
				State:        "accepted",
				AttemptCount: 0,
			}),
		},
	}

	store := &workerStore{db: db}

	_, err := store.CompleteProcessingJob(
		context.Background(),
		801,
		901,
	)

	if !errors.Is(err, errProcessingJobConflict) {
		t.Fatalf(
			"expected errProcessingJobConflict, got %v",
			err,
		)
	}
}

func TestRecordProcessingFailureKeepsJobRetryableBeforeLimit(
	t *testing.T,
) {
	errorCode := "processing_failed"

	db := &workerStubDB{
		rows: []pgx.Row{
			workerJobRow(workerProcessingJob{
				ID:           1001,
				WorkItemID:   2001,
				State:        "accepted",
				AttemptCount: 1,
				LastError:    &errorCode,
			}),
		},
	}

	store := &workerStore{db: db}

	result, err := store.RecordProcessingFailure(
		context.Background(),
		1001,
		2001,
		errorCode,
		3,
	)
	if err != nil {
		t.Fatalf(
			"record retryable processing failure: %v",
			err,
		)
	}

	if result.Disposition != processingFailureRetryable {
		t.Fatalf(
			"expected %q, got %q",
			processingFailureRetryable,
			result.Disposition,
		)
	}

	if result.Job.State != "accepted" {
		t.Fatalf(
			"retryable failure changed state to %q",
			result.Job.State,
		)
	}

	if result.Job.AttemptCount != 1 {
		t.Fatalf(
			"expected attempt_count=1, got %d",
			result.Job.AttemptCount,
		)
	}

	if result.Job.LastError == nil ||
		*result.Job.LastError != errorCode {
		t.Fatalf(
			"expected last_error_code=%q, got %#v",
			errorCode,
			result.Job.LastError,
		)
	}

	if result.Job.FinishedAt != nil {
		t.Fatal(
			"retryable failure unexpectedly set finished_at",
		)
	}

	if len(db.calls) != 1 {
		t.Fatalf(
			"expected one database query, got %d",
			len(db.calls),
		)
	}

	if !strings.Contains(
		db.calls[0].query,
		"attempt_count = attempt_count + 1",
	) {
		t.Fatal(
			"failure update does not durably increment attempt_count",
		)
	}

	if !strings.Contains(
		db.calls[0].query,
		"attempt_count + 1 >= $4",
	) {
		t.Fatal(
			"failure update does not atomically enforce retry boundary",
		)
	}
}

func TestRecordProcessingFailureMarksJobFailedAtLimit(
	t *testing.T,
) {
	errorCode := "processing_failed"
	finishedAt := time.Now().UTC()

	db := &workerStubDB{
		rows: []pgx.Row{
			workerJobRow(workerProcessingJob{
				ID:           3001,
				WorkItemID:   4001,
				State:        "failed",
				AttemptCount: 3,
				LastError:    &errorCode,
				FinishedAt:   &finishedAt,
			}),
		},
	}

	store := &workerStore{db: db}

	result, err := store.RecordProcessingFailure(
		context.Background(),
		3001,
		4001,
		errorCode,
		3,
	)
	if err != nil {
		t.Fatalf(
			"record exhausted processing failure: %v",
			err,
		)
	}

	if result.Disposition != processingFailureTerminal {
		t.Fatalf(
			"expected %q, got %q",
			processingFailureTerminal,
			result.Disposition,
		)
	}

	if result.Job.State != "failed" {
		t.Fatalf(
			"expected failed state, got %q",
			result.Job.State,
		)
	}

	if result.Job.AttemptCount != 3 {
		t.Fatalf(
			"expected attempt_count=3, got %d",
			result.Job.AttemptCount,
		)
	}

	if result.Job.LastError == nil ||
		*result.Job.LastError != errorCode {
		t.Fatalf(
			"expected last_error_code=%q, got %#v",
			errorCode,
			result.Job.LastError,
		)
	}

	if result.Job.FinishedAt == nil {
		t.Fatal(
			"terminal processing failure did not set finished_at",
		)
	}
}

func TestRecordProcessingFailureClassifiesTerminalRedelivery(
	t *testing.T,
) {
	errorCode := "processing_failed"
	finishedAt := time.Now().UTC()

	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(pgx.ErrNoRows),
			workerJobRow(workerProcessingJob{
				ID:           5001,
				WorkItemID:   6001,
				State:        "failed",
				AttemptCount: 3,
				LastError:    &errorCode,
				FinishedAt:   &finishedAt,
			}),
		},
	}

	store := &workerStore{db: db}

	result, err := store.RecordProcessingFailure(
		context.Background(),
		5001,
		6001,
		errorCode,
		3,
	)
	if err != nil {
		t.Fatalf(
			"classify terminal failure redelivery: %v",
			err,
		)
	}

	if result.Disposition != processingFailureAlreadyTerminal {
		t.Fatalf(
			"expected %q, got %q",
			processingFailureAlreadyTerminal,
			result.Disposition,
		)
	}

	if result.Job.AttemptCount != 3 {
		t.Fatalf(
			"terminal redelivery changed attempt_count: %d",
			result.Job.AttemptCount,
		)
	}

	if len(db.calls) != 2 {
		t.Fatalf(
			"expected guarded update plus lookup, got %d queries",
			len(db.calls),
		)
	}
}

func TestRecordProcessingFailureDoesNotInventAttemptOnDatabaseError(
	t *testing.T,
) {
	databaseErr := errors.New(
		"PostgreSQL temporarily unavailable",
	)

	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(databaseErr),
		},
	}

	store := &workerStore{db: db}

	_, err := store.RecordProcessingFailure(
		context.Background(),
		7001,
		8001,
		"processing_failed",
		3,
	)

	if !errors.Is(err, databaseErr) {
		t.Fatalf(
			"expected original database error %v, got %v",
			databaseErr,
			err,
		)
	}

	if len(db.calls) != 1 {
		t.Fatalf(
			"database failure unexpectedly triggered %d queries",
			len(db.calls),
		)
	}
}

func TestRecordProcessingFailureValidatesRetryPolicy(
	t *testing.T,
) {
	store := &workerStore{
		db: &workerStubDB{},
	}

	_, err := store.RecordProcessingFailure(
		context.Background(),
		1,
		2,
		"processing_failed",
		0,
	)
	if err == nil {
		t.Fatal(
			"non-positive max attempts unexpectedly accepted",
		)
	}

	_, err = store.RecordProcessingFailure(
		context.Background(),
		1,
		2,
		"",
		3,
	)
	if err == nil {
		t.Fatal(
			"empty failure code unexpectedly accepted",
		)
	}
}

func TestGetProcessingJobReturnsMatchingJob(
	t *testing.T,
) {
	db := &workerStubDB{
		rows: []pgx.Row{
			workerJobRow(workerProcessingJob{
				ID:           9001,
				WorkItemID:   9002,
				State:        "accepted",
				AttemptCount: 2,
			}),
		},
	}

	store := &workerStore{
		db: db,
	}

	job, err := store.GetProcessingJob(
		context.Background(),
		9001,
		9002,
	)
	if err != nil {
		t.Fatalf(
			"get processing job: %v",
			err,
		)
	}

	if job.ID != 9001 ||
		job.WorkItemID != 9002 ||
		job.State != "accepted" ||
		job.AttemptCount != 2 {
		t.Fatalf(
			"unexpected processing job: %+v",
			job,
		)
	}
}

func TestGetProcessingJobRejectsUnknownJob(
	t *testing.T,
) {
	db := &workerStubDB{
		rows: []pgx.Row{
			workerErrorRow(pgx.ErrNoRows),
		},
	}

	store := &workerStore{
		db: db,
	}

	_, err := store.GetProcessingJob(
		context.Background(),
		9101,
		9102,
	)

	if !errors.Is(
		err,
		errProcessingJobNotFound,
	) {
		t.Fatalf(
			"expected not-found error, got %v",
			err,
		)
	}
}

func TestGetProcessingJobRejectsWorkItemMismatch(
	t *testing.T,
) {
	db := &workerStubDB{
		rows: []pgx.Row{
			workerJobRow(workerProcessingJob{
				ID:         9201,
				WorkItemID: 9202,
				State:      "accepted",
			}),
		},
	}

	store := &workerStore{
		db: db,
	}

	_, err := store.GetProcessingJob(
		context.Background(),
		9201,
		9999,
	)

	if !errors.Is(
		err,
		errProcessingJobMismatch,
	) {
		t.Fatalf(
			"expected identity mismatch, got %v",
			err,
		)
	}
}
