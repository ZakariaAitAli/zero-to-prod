package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
)

const itemLockWaitersSQL = `SELECT count(*) FROM pg_locks waiter
 JOIN pg_locks holder ON holder.locktype = 'advisory' AND holder.granted AND holder.pid = $1
  AND holder.classid = waiter.classid AND holder.objid = waiter.objid AND holder.objsubid = waiter.objsubid
 WHERE waiter.locktype = 'advisory' AND NOT waiter.granted`

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, err)
	}
}

// deleteWorkItemGraph removes every row for one Work Item in one statement so
// the deferred success constraints are satisfied at commit.
func deleteWorkItemGraph(t *testing.T, pool *pgxpool.Pool, workItemID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `WITH r AS (DELETE FROM public.work_item_results WHERE work_item_id=$1),
 o AS (DELETE FROM public.outbox_messages WHERE processing_job_id IN (SELECT id FROM public.processing_jobs WHERE work_item_id=$1)),
 j AS (DELETE FROM public.processing_jobs WHERE work_item_id=$1)
 DELETE FROM public.work_items WHERE id=$1`, workItemID); err != nil {
		t.Errorf("clean Work Item %d: %v", workItemID, err)
	}
}

func TestDatabaseEnforcesSingleResultAfterCompletionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	if _, err := fixture.Store.CompleteProcessingJob(ctx, fixture.JobID, fixture.WorkItemID); err != nil {
		t.Fatal(err)
	}
	workerPool := fixture.Store.db.(*pgxpool.Pool)
	// The worker role can insert results, but not a second one for the item.
	_, err := workerPool.Exec(ctx, `INSERT INTO public.work_item_results
 (work_item_id, processing_job_id, input_title, analysis_version, character_count, word_count)
 SELECT work_item_id, id, 'duplicate', 1, 9, 1 FROM public.processing_jobs WHERE id=$1`, fixture.JobID)
	requireSQLState(t, err, "23505")
	// A done item cannot gain another accepted job, even below the API guard.
	_, err = fixture.Pool.Exec(ctx, "INSERT INTO public.processing_jobs (work_item_id) VALUES ($1)", fixture.WorkItemID)
	requireSQLState(t, err, "23505")
	requireCompletedOnce(t, requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID), fixture.JobID)
}

func TestConcurrentCompletionCommitsOnceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	const completers = 6
	config, err := pgxpool.ParseConfig(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = completers
	workerPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer workerPool.Close()
	store := &workerStore{db: workerPool}

	// Hold the per-item lock so every completer is provably inside its
	// transaction, waiting on the same lock, before any of them proceeds.
	holder, err := fixture.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	var holderPID uint32
	if err := holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock(1464423501, hashint8($1::bigint))", fixture.WorkItemID); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		completion processingCompletion
		err        error
	}
	outcomes := make(chan outcome, completers)
	var wg sync.WaitGroup
	for range completers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			completion, err := store.CompleteProcessingJob(ctx, fixture.JobID, fixture.WorkItemID)
			outcomes <- outcome{completion, err}
		}()
	}

	for waiters := 0; waiters != completers; {
		if err := fixture.Pool.QueryRow(ctx, itemLockWaitersSQL, holderPID).Scan(&waiters); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatalf("only %d of %d completers reached the item lock", waiters, completers)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock(1464423501, hashint8($1::bigint))", fixture.WorkItemID); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(outcomes)

	applied, terminal := 0, 0
	for result := range outcomes {
		switch {
		case result.err != nil:
			t.Fatalf("concurrent completion failed: %v", result.err)
		case result.completion.Disposition == completionApplied:
			applied++
		case result.completion.Disposition == completionAlreadyTerminal:
			terminal++
		}
	}
	if applied != 1 || terminal != completers-1 {
		t.Fatalf("applied=%d already_terminal=%d", applied, terminal)
	}
	requireCompletedOnce(t, requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID), fixture.JobID)
}

// Processor failures below are injected with integrationFailingProcessor, a
// test-only processor. The production title analysis has no failure path.
func TestInjectedRetryExhaustionThenNewJobSucceedsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	t.Cleanup(func() { deleteWorkItemGraph(t, fixture.Pool, fixture.WorkItemID) })

	failing := &integrationFailingProcessor{err: errors.New("injected test-only processing failure")}
	firstPayload := workerPayload(fixture.JobID, fixture.WorkItemID)
	expected := []deliverySettlement{settlementNackRequeue, settlementNackRequeue, settlementAck}
	for attempt, want := range expected {
		settlement, _ := handleWorkerMessageWithProcessor(ctx, fixture.Store, failing, firstPayload)
		if settlement != want {
			t.Fatalf("failure %d: expected %q, got %q", attempt+1, want, settlement)
		}
	}
	exhausted := requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID)
	first, _ := exhausted.job(fixture.JobID)
	if exhausted.Status != "pending" || len(exhausted.Results) != 0 || first.State != "failed" ||
		first.AttemptCount != workerProcessingMaxAttempts || !first.Finished ||
		first.LastError == nil || *first.LastError != workerProcessingFailureCode {
		t.Fatalf("unexpected exhausted state: %s", exhausted)
	}

	// A new explicit request after terminal failure creates a new job; the API
	// path for that request is covered by the API acceptance integration test.
	var secondJobID int64
	if err := fixture.Pool.QueryRow(ctx, "INSERT INTO public.processing_jobs (work_item_id) VALUES ($1) RETURNING id",
		fixture.WorkItemID).Scan(&secondJobID); err != nil {
		t.Fatal(err)
	}
	settlement, err := handleWorkerMessage(ctx, fixture.Store, workerPayload(secondJobID, fixture.WorkItemID))
	if err != nil || settlement != settlementAck {
		t.Fatalf("second job: settlement=%q err=%v", settlement, err)
	}
	// A late redelivery of the exhausted job's message changes nothing.
	settlement, err = handleWorkerMessage(ctx, fixture.Store, firstPayload)
	if err != nil || settlement != settlementAck {
		t.Fatalf("late first-job delivery: settlement=%q err=%v", settlement, err)
	}

	final := requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID)
	requireCompletedOnce(t, final, secondJobID)
	if job, _ := final.job(fixture.JobID); job.State != first.State || job.AttemptCount != first.AttemptCount ||
		job.LastError == nil || *job.LastError != *first.LastError {
		t.Fatalf("exhausted job changed after later success: %s", final)
	}
}

func receiveDelivery(t *testing.T, ctx context.Context, session *rabbitMQWorkerSession) amqp.Delivery {
	t.Helper()
	select {
	case delivery, ok := <-session.Deliveries():
		if !ok {
			t.Fatal("delivery stream closed")
		}
		return delivery
	case <-ctx.Done():
		t.Fatalf("timed out waiting for delivery: %v", ctx.Err())
	}
	return amqp.Delivery{}
}

func requireQueueDrained(t *testing.T, fixtureURL, queueName string) {
	t.Helper()
	queue, err := inspectQueue(fixtureURL, queueName)
	if err != nil {
		t.Fatal(err)
	}
	if queue.Messages != 0 {
		t.Fatalf("expected drained queue, found %d ready messages", queue.Messages)
	}
}

type injectedFailuresProcessor struct{ remaining int }

func (processor *injectedFailuresProcessor) Process(context.Context, workerMessage) error {
	if processor.remaining > 0 {
		processor.remaining--
		return errors.New("injected test-only transient failure")
	}
	return nil
}

func TestInjectedTransientFailuresRequeueThroughRabbitMQIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := requireWorkerRabbitMQIntegrationFixture(t, ctx)
	session, err := newRabbitMQWorkerSession(ctx, fixture.WorkerURL, fixture.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	processor := &injectedFailuresProcessor{remaining: 2}
	expected := []deliverySettlement{settlementNackRequeue, settlementNackRequeue, settlementAck}
	for index, want := range expected {
		delivery := receiveDelivery(t, ctx, session)
		if delivery.Redelivered != (index > 0) {
			t.Fatalf("delivery %d: redelivered=%t", index+1, delivery.Redelivered)
		}
		result, err := processRabbitMQDeliveryWithProcessor(ctx, fixture.Store, processor, delivery)
		if err != nil || result.Settlement != want {
			t.Fatalf("delivery %d: settlement=%q err=%v", index+1, result.Settlement, err)
		}
		if index < 2 {
			state := requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID)
			job, _ := state.job(fixture.JobID)
			if state.Status != "pending" || job.State != "accepted" || job.AttemptCount != index+1 {
				t.Fatalf("after injected failure %d: %s", index+1, state)
			}
		}
	}
	requireQueueDrained(t, fixture.FixtureURL, fixture.QueueName)
	state := requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID)
	job, _ := state.job(fixture.JobID)
	if state.Status != "done" || job.State != "succeeded" || job.AttemptCount != 3 || job.LastError != nil {
		t.Fatalf("unexpected state after retries: %s", state)
	}
}

func TestInjectedRetryExhaustionThroughRabbitMQIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := requireWorkerRabbitMQIntegrationFixture(t, ctx)
	session, err := newRabbitMQWorkerSession(ctx, fixture.WorkerURL, fixture.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	processor := &injectedFailuresProcessor{remaining: workerProcessingMaxAttempts}
	expected := []deliverySettlement{settlementNackRequeue, settlementNackRequeue, settlementAck}
	for index, want := range expected {
		result, err := processRabbitMQDeliveryWithProcessor(ctx, fixture.Store, processor, receiveDelivery(t, ctx, session))
		if err != nil || result.Settlement != want {
			t.Fatalf("delivery %d: settlement=%q err=%v", index+1, result.Settlement, err)
		}
	}
	requireQueueDrained(t, fixture.FixtureURL, fixture.QueueName)
	state := requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID)
	job, _ := state.job(fixture.JobID)
	if state.Status != "pending" || len(state.Results) != 0 || job.State != "failed" || job.AttemptCount != 3 {
		t.Fatalf("unexpected exhausted state: %s", state)
	}
}

// Duplicate publication is simulated by publishing the fixture payload twice.
// This does not reproduce the outbox publisher crash window itself.
func TestDuplicatePublicationCompletesOnceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture := requireWorkerRabbitMQIntegrationFixture(t, ctx)
	if err := publishFixturePayload(ctx, fixture.FixtureURL, fixture.QueueName, fixture.Payload); err != nil {
		t.Fatal(err)
	}
	session, err := newRabbitMQWorkerSession(ctx, fixture.WorkerURL, fixture.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for index := range 2 {
		result, err := processRabbitMQDelivery(ctx, fixture.Store, receiveDelivery(t, ctx, session))
		if err != nil || result.Settlement != settlementAck || result.HandlingErr != nil {
			t.Fatalf("copy %d: settlement=%q handling=%v err=%v", index+1, result.Settlement, result.HandlingErr, err)
		}
	}
	requireQueueDrained(t, fixture.FixtureURL, fixture.QueueName)
	requireCompletedOnce(t, requireBusinessState(t, ctx, fixture.Pool, fixture.WorkItemID), fixture.JobID)
}
