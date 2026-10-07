package main

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/workitems"
)

func analyzeTitle(title string) (int, int) {
	return utf8.RuneCountInString(title), len(strings.Fields(title))
}

func (store *workerStore) CompleteProcessingJob(ctx context.Context, jobID, workItemID int64) (processingCompletion, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return processingCompletion{}, fmt.Errorf("begin business completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, workitems.ItemLockSQL, workItemID); err != nil {
		return processingCompletion{}, err
	}
	transactionalStore := &workerStore{db: tx}
	job, err := transactionalStore.GetProcessingJob(ctx, jobID, workItemID)
	if err != nil {
		return processingCompletion{}, err
	}
	if job.State == "succeeded" || job.State == "failed" {
		return processingCompletion{Disposition: completionAlreadyTerminal, Job: job}, nil
	}
	var title, status string
	if err = tx.QueryRow(ctx, "SELECT title, status FROM public.work_items WHERE id=$1", workItemID).Scan(&title, &status); err != nil {
		return processingCompletion{}, err
	}
	if status != workitems.Pending {
		return processingCompletion{}, errWorkItemNotPending
	}
	characters, words := analyzeTitle(title)
	if _, err = tx.Exec(ctx, `INSERT INTO public.work_item_results
 (work_item_id, processing_job_id, input_title, analysis_version, character_count, word_count)
 VALUES ($1,$2,$3,1,$4,$5)`, workItemID, jobID, title, characters, words); err != nil {
		return processingCompletion{}, err
	}
	result, err := tx.Exec(ctx, "UPDATE public.work_items SET status=$2 WHERE id=$1 AND status=$3", workItemID, workitems.Done, workitems.Pending)
	if err != nil {
		return processingCompletion{}, err
	}
	if result.RowsAffected() != 1 {
		return processingCompletion{}, errProcessingJobConflict
	}
	job, err = scanWorkerProcessingJob(tx.QueryRow(ctx, `UPDATE public.processing_jobs
 SET state='succeeded', attempt_count=attempt_count+1, last_error_code=NULL, finished_at=CURRENT_TIMESTAMP
 WHERE id=$1 AND work_item_id=$2 AND state='accepted'
 RETURNING id, work_item_id, state, attempt_count, last_error_code, finished_at`, jobID, workItemID))
	if err != nil {
		return processingCompletion{}, fmt.Errorf("guarded job completion: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return processingCompletion{}, fmt.Errorf("commit business completion (outcome may be uncertain): %w", err)
	}
	return processingCompletion{Disposition: completionApplied, Job: job}, nil
}
