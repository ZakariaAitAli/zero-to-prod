package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func TestProcessingAcceptanceConflictIntegration(t *testing.T) {
	url := os.Getenv("OUTBOX_INTEGRATION_DATABASE_URL")
	adminURL := os.Getenv("ACCEPTANCE_FAILURE_MIGRATOR_DATABASE_URL")
	if url == "" || adminURL == "" {
		t.Skip("integration database URLs required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := newPostgresStore(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	item, err := store.CreateWorkItem(ctx, "Review API design", "pending")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, err := pool.Exec(context.Background(), `WITH o AS (DELETE FROM public.outbox_messages WHERE processing_job_id IN (SELECT id FROM public.processing_jobs WHERE work_item_id=$1)), r AS (DELETE FROM public.work_item_results WHERE work_item_id=$1), j AS (DELETE FROM public.processing_jobs WHERE work_item_id=$1) DELETE FROM public.work_items WHERE id=$1`, item.ID)
		if err != nil {
			t.Error(err)
		}
	}()
	type outcome struct {
		job processingJob
		err error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := store.AcceptProcessingJob(ctx, item.ID)
			outcomes <- outcome{job, err}
		}()
	}
	wg.Wait()
	close(outcomes)
	var accepted, conflict int
	var winner, active int64
	for result := range outcomes {
		if result.err == nil {
			accepted++
			winner = result.job.ID
		} else if errors.Is(result.err, errProcessingAlreadyActive) {
			conflict++
			active = result.job.ID
		} else {
			t.Fatal(result.err)
		}
	}
	if accepted != 1 || conflict != 1 || winner != active {
		t.Fatalf("accepted=%d conflict=%d winner=%d active=%d", accepted, conflict, winner, active)
	}
	var jobs, outbox int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM public.processing_jobs WHERE work_item_id=$1),(SELECT count(*) FROM public.outbox_messages WHERE processing_job_id=$2)`, item.ID, winner).Scan(&jobs, &outbox); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || outbox != 1 {
		t.Fatalf("jobs/outbox %d/%d", jobs, outbox)
	}
	// A terminal failure permits a new explicit request.
	if _, err := pool.Exec(ctx, "UPDATE public.processing_jobs SET state='failed',finished_at=now() WHERE id=$1", winner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptProcessingJob(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH r AS (INSERT INTO public.work_item_results(work_item_id,processing_job_id,input_title,analysis_version,character_count,word_count)
 SELECT work_item_id,id,'Review API design',1,17,3 FROM public.processing_jobs WHERE work_item_id=$1 AND state='accepted'),
 j AS (UPDATE public.processing_jobs SET state='succeeded',finished_at=now() WHERE work_item_id=$1 AND state='accepted')
 UPDATE public.work_items SET status='done' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptProcessingJob(ctx, item.ID); !errors.Is(err, errWorkItemDone) {
		t.Fatalf("expected completed conflict, got %v", err)
	}
	// Runtime API role cannot create done items even if handler validation is bypassed.
	_, err = store.pool.Exec(ctx, "INSERT INTO public.work_items(title,status) VALUES ('invalid','done')")
	var permissionError *pgconn.PgError
	if !errors.As(err, &permissionError) || permissionError.Code != "42501" {
		t.Fatalf("expected insufficient privilege (42501), got %v", err)
	}
}
