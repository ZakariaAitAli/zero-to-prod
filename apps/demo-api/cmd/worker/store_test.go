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
