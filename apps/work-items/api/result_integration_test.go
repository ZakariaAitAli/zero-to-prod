package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
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

type httpAcceptanceOutcome struct {
	status int
	body   map[string]any
	err    error
}

// postProcessingConcurrently releases all requests at once and returns their
// outcomes. Overlap is established separately through PostgreSQL lock waits.
func postProcessingConcurrently(serverURL string, workItemID int64, requests int) []httpAcceptanceOutcome {
	start := make(chan struct{})
	outcomes := make([]httpAcceptanceOutcome, requests)
	var wg sync.WaitGroup
	for index := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			response, err := http.Post(fmt.Sprintf("%s/items/%d/process", serverURL, workItemID), "application/json", nil)
			if err != nil {
				outcomes[index].err = err
				return
			}
			defer response.Body.Close()
			outcomes[index].status = response.StatusCode
			outcomes[index].err = json.NewDecoder(response.Body).Decode(&outcomes[index].body)
		}()
	}
	close(start)
	wg.Wait()
	return outcomes
}

func TestConcurrentHTTPProcessingAcceptanceIntegration(t *testing.T) {
	url := os.Getenv("OUTBOX_INTEGRATION_DATABASE_URL")
	adminURL := os.Getenv("ACCEPTANCE_FAILURE_MIGRATOR_DATABASE_URL")
	if url == "" || adminURL == "" {
		t.Skip("integration database URLs required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	readiness := &readinessState{}
	readiness.set(true)
	server := httptest.NewServer(newHandler("test", readiness, store))
	defer server.Close()

	item, err := store.CreateWorkItem(ctx, "Concurrent acceptance", "pending")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, err := pool.Exec(context.Background(), `WITH o AS (DELETE FROM public.outbox_messages WHERE processing_job_id IN (SELECT id FROM public.processing_jobs WHERE work_item_id=$1)), r AS (DELETE FROM public.work_item_results WHERE work_item_id=$1), j AS (DELETE FROM public.processing_jobs WHERE work_item_id=$1) DELETE FROM public.work_items WHERE id=$1`, item.ID)
		if err != nil {
			t.Error(err)
		}
	}()

	const requests = 32
	// Hold the per-item lock until every pooled API connection is waiting on
	// it, proving the acceptance transactions overlap.
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	var holderPID uint32
	if err := holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock(1464423501, hashint8($1::bigint))", item.ID); err != nil {
		t.Fatal(err)
	}
	expectedWaiters := min(int(store.pool.Config().MaxConns), requests)
	results := make(chan []httpAcceptanceOutcome, 1)
	go func() { results <- postProcessingConcurrently(server.URL, item.ID, requests) }()
	for waiters := 0; waiters < expectedWaiters; {
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks waiter
 JOIN pg_locks holder ON holder.locktype='advisory' AND holder.granted AND holder.pid=$1
  AND holder.classid=waiter.classid AND holder.objid=waiter.objid AND holder.objsubid=waiter.objsubid
 WHERE waiter.locktype='advisory' AND NOT waiter.granted`, holderPID).Scan(&waiters); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatalf("only %d of %d acceptance transactions overlapped", waiters, expectedWaiters)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock(1464423501, hashint8($1::bigint))", item.ID); err != nil {
		t.Fatal(err)
	}

	accepted, conflicts := 0, 0
	var acceptedID float64
	activeIDs := map[float64]int{}
	for _, outcome := range <-results {
		switch {
		case outcome.err != nil:
			t.Fatal(outcome.err)
		case outcome.status == http.StatusAccepted:
			accepted++
			acceptedID, _ = outcome.body["id"].(float64)
		case outcome.status == http.StatusConflict && outcome.body["error"] == "processing_already_active":
			conflicts++
			id, _ := outcome.body["processing_job_id"].(float64)
			activeIDs[id]++
		default:
			t.Fatalf("unexpected response %d %v", outcome.status, outcome.body)
		}
	}
	if accepted != 1 || conflicts != requests-1 || len(activeIDs) != 1 || activeIDs[acceptedID] != requests-1 {
		t.Fatalf("accepted=%d conflicts=%d accepted_id=%v active_ids=%v", accepted, conflicts, acceptedID, activeIDs)
	}
	countRows := func() (jobs, outbox int) {
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM public.processing_jobs WHERE work_item_id=$1),
 (SELECT count(*) FROM public.outbox_messages WHERE processing_job_id IN (SELECT id FROM public.processing_jobs WHERE work_item_id=$1))`,
			item.ID).Scan(&jobs, &outbox); err != nil {
			t.Fatal(err)
		}
		return jobs, outbox
	}
	if jobs, outbox := countRows(); jobs != 1 || outbox != 1 {
		t.Fatalf("jobs/outbox %d/%d", jobs, outbox)
	}

	// Complete the item through the same transactional writes as the worker.
	if _, err := pool.Exec(ctx, `WITH r AS (INSERT INTO public.work_item_results(work_item_id,processing_job_id,input_title,analysis_version,character_count,word_count)
 SELECT work_item_id,id,'Concurrent acceptance',1,21,2 FROM public.processing_jobs WHERE work_item_id=$1 AND state='accepted'),
 j AS (UPDATE public.processing_jobs SET state='succeeded',attempt_count=1,finished_at=now() WHERE work_item_id=$1 AND state='accepted')
 UPDATE public.work_items SET status='done' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range postProcessingConcurrently(server.URL, item.ID, requests) {
		if outcome.err != nil || outcome.status != http.StatusConflict || outcome.body["error"] != "work_item_already_done" {
			t.Fatalf("done item request: %d %v %v", outcome.status, outcome.body, outcome.err)
		}
	}
	if jobs, outbox := countRows(); jobs != 1 || outbox != 1 {
		t.Fatalf("done-item requests created rows: jobs/outbox %d/%d", jobs, outbox)
	}
}
