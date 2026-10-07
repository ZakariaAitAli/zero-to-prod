package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAnalyzeTitle(t *testing.T) {
	for _, test := range []struct {
		title             string
		characters, words int
	}{
		{"Review API design", 17, 3},
		{"é\u2003猫\u00a0🙂", 5, 3},
		{"a,b", 3, 1},
		{"e\u0301", 2, 1},
		{"", 0, 0},
	} {
		characters, words := analyzeTitle(test.title)
		if characters != test.characters || words != test.words {
			t.Fatalf("%q: got %d/%d, want %d/%d", test.title, characters, words, test.characters, test.words)
		}
	}
}

// Exercise transaction decisions without requiring a live database. SQL and
// constraints themselves are verified by the PostgreSQL integration suite.
type completionTx struct {
	pgx.Tx
	db                 workerStubDB
	executions         [][]any
	updateRows         int64
	commitErr          error
	commits, rollbacks int
}

func (tx *completionTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return tx.db.QueryRow(ctx, query, args...)
}
func (tx *completionTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.executions = append(tx.executions, args)
	if len(tx.executions) == 3 {
		return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", tx.updateRows)), nil
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (tx *completionTx) Commit(context.Context) error   { tx.commits++; return tx.commitErr }
func (tx *completionTx) Rollback(context.Context) error { tx.rollbacks++; return nil }

func titleRow(status string) pgx.Row {
	return workerStubRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "Review API design"
		*(dest[1].(*string)) = status
		return nil
	}}
}

func TestCompletionTransactionDecisions(t *testing.T) {
	uncertain := errors.New("connection lost during commit")
	for _, test := range []struct {
		name, state, status                      string
		updateRows                               int64
		lookupErr, updateErr, commitErr, wantErr error
		wantDisposition                          completionDisposition
		wantCommits                              int
	}{
		{name: "success", state: "accepted", status: "pending", updateRows: 1, wantDisposition: completionApplied, wantCommits: 1},
		{name: "successful duplicate", state: "succeeded", wantDisposition: completionAlreadyTerminal},
		{name: "failed duplicate", state: "failed", wantDisposition: completionAlreadyTerminal},
		{name: "unknown job", lookupErr: pgx.ErrNoRows, wantErr: errProcessingJobNotFound},
		{name: "inconsistent item", state: "accepted", status: "done", wantErr: errWorkItemNotPending},
		{name: "item guard loses race", state: "accepted", status: "pending", updateRows: 0, wantErr: errProcessingJobConflict},
		{name: "job guard loses race", state: "accepted", status: "pending", updateRows: 1, updateErr: pgx.ErrNoRows, wantErr: pgx.ErrNoRows},
		{name: "uncertain commit", state: "accepted", status: "pending", updateRows: 1, commitErr: uncertain, wantErr: uncertain, wantCommits: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			job := workerProcessingJob{ID: 7, WorkItemID: 42, State: test.state}
			tx := &completionTx{updateRows: test.updateRows, commitErr: test.commitErr}
			if test.lookupErr != nil {
				tx.db.rows = append(tx.db.rows, workerErrorRow(test.lookupErr))
			} else {
				tx.db.rows = append(tx.db.rows, workerJobRow(job))
			}
			tx.db.rows = append(tx.db.rows, titleRow(test.status))
			if test.updateErr != nil {
				tx.db.rows = append(tx.db.rows, workerErrorRow(test.updateErr))
			} else {
				job.State = "succeeded"
				tx.db.rows = append(tx.db.rows, workerJobRow(job))
			}
			store := &workerStore{db: &workerStubDB{tx: tx}}
			result, err := store.CompleteProcessingJob(context.Background(), 7, 42)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("got %v, want %v", err, test.wantErr)
			}
			if result.Disposition != test.wantDisposition {
				t.Fatalf("disposition %q, want %q", result.Disposition, test.wantDisposition)
			}
			if tx.commits != test.wantCommits || tx.rollbacks != 1 {
				t.Fatalf("commits=%d rollbacks=%d", tx.commits, tx.rollbacks)
			}
			if test.name == "success" {
				insertArgs := tx.executions[1]
				if insertArgs[2] != "Review API design" || insertArgs[3] != 17 || insertArgs[4] != 3 {
					t.Fatalf("incorrect business result: %v", insertArgs)
				}
			}
		})
	}
}

func TestCompletionBeginFailureDoesNotAttemptTransaction(t *testing.T) {
	failure := errors.New("database unavailable")
	store := &workerStore{db: &workerStubDB{beginErr: failure}}
	if _, err := store.CompleteProcessingJob(context.Background(), 7, 42); !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
}

func TestInconsistentBusinessStateRejectsDelivery(t *testing.T) {
	store := &stubProcessingJobCompleter{err: fmt.Errorf("completion: %w", errWorkItemNotPending)}
	settlement, err := handleWorkerMessage(context.Background(), store, []byte(`{"type":"work_item.process","version":1,"job_id":7,"work_item_id":42}`))
	if settlement != settlementReject || !errors.Is(err, errWorkItemNotPending) {
		t.Fatalf("settlement=%s err=%v", settlement, err)
	}
}
