package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCompleteProcessingJobPostgresIntegration(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	fixture := requireWorkerPostgresIntegrationFixture(
		t,
		ctx,
	)

	store := fixture.Store
	jobID := fixture.JobID
	workItemID := fixture.WorkItemID

	first, err := store.CompleteProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf("complete accepted job: %v", err)
	}

	if first.Disposition != completionApplied {
		t.Fatalf(
			"expected first completion disposition %q, got %q",
			completionApplied,
			first.Disposition,
		)
	}

	if first.Job.State != "succeeded" {
		t.Fatalf(
			"expected first completion state succeeded, got %q",
			first.Job.State,
		)
	}

	if first.Job.AttemptCount != 1 {
		t.Fatalf(
			"expected first completion attempt_count=1, got %d",
			first.Job.AttemptCount,
		)
	}

	var title, status string
	var characters, words, version int
	var producingJob int64
	if err := fixture.Pool.QueryRow(ctx, `SELECT i.status, r.input_title, r.character_count, r.word_count, r.analysis_version, r.processing_job_id
 FROM public.work_items i JOIN public.work_item_results r ON r.work_item_id=i.id WHERE i.id=$1`, workItemID).Scan(&status, &title, &characters, &words, &version, &producingJob); err != nil {
		t.Fatal(err)
	}
	expectedCharacters, expectedWords := analyzeTitle(title)
	if status != "done" || characters != expectedCharacters || words != expectedWords || version != 1 || producingJob != jobID {
		t.Fatalf("invalid result: %s %d %d %d %d", status, characters, words, version, producingJob)
	}
	second, err := store.CompleteProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf("classify duplicate completion: %v", err)
	}

	if second.Disposition != completionAlreadyTerminal {
		t.Fatalf(
			"expected duplicate disposition %q, got %q",
			completionAlreadyTerminal,
			second.Disposition,
		)
	}

	if second.Job.State != "succeeded" {
		t.Fatalf(
			"expected duplicate state succeeded, got %q",
			second.Job.State,
		)
	}

	if second.Job.AttemptCount != 1 {
		t.Fatalf(
			"duplicate delivery changed attempt_count: %d",
			second.Job.AttemptCount,
		)
	}
}

func TestCompletionRollsBackWhenJobGuardFailsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	// Returning NULL suppresses the guarded job update after the result and item writes.
	_, err := fixture.Pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION public.issue117_skip_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id=%d THEN RETURN NULL; END IF; RETURN NEW; END $$;
 CREATE TRIGGER issue117_skip_completion BEFORE UPDATE ON public.processing_jobs FOR EACH ROW EXECUTE FUNCTION public.issue117_skip_completion();`, fixture.JobID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, err := fixture.Pool.Exec(context.Background(), `DROP TRIGGER issue117_skip_completion ON public.processing_jobs; DROP FUNCTION public.issue117_skip_completion();`)
		if err != nil {
			t.Error(err)
		}
	}()
	if _, err := fixture.Store.CompleteProcessingJob(ctx, fixture.JobID, fixture.WorkItemID); err == nil {
		t.Fatal("suppressed update committed")
	}
	var status, state string
	var results int
	if err := fixture.Pool.QueryRow(ctx, `SELECT i.status,j.state,(SELECT count(*) FROM public.work_item_results WHERE work_item_id=i.id) FROM public.work_items i JOIN public.processing_jobs j ON j.work_item_id=i.id WHERE j.id=$1`, fixture.JobID).Scan(&status, &state, &results); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || state != "accepted" || results != 0 {
		t.Fatalf("partial completion: %s %s %d", status, state, results)
	}
}

func TestCompletionRejectsWrongJobIdentityIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	if _, err := fixture.Store.CompleteProcessingJob(ctx, fixture.JobID, fixture.WorkItemID+100000); !errors.Is(err, errProcessingJobMismatch) {
		t.Fatalf("expected mismatch, got %v", err)
	}
	if _, err := fixture.Store.CompleteProcessingJob(ctx, fixture.JobID+100000, fixture.WorkItemID); !errors.Is(err, errProcessingJobNotFound) {
		t.Fatalf("expected unknown job, got %v", err)
	}
}

func TestDatabaseRejectsPartialBusinessSuccessIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fixture := requireWorkerPostgresIntegrationFixture(t, ctx)
	for _, query := range []string{
		"UPDATE public.work_items SET status='done' WHERE id=$1",
		"UPDATE public.processing_jobs SET state='succeeded',finished_at=now() WHERE work_item_id=$1",
		"INSERT INTO public.work_item_results(work_item_id,processing_job_id,input_title,analysis_version,character_count,word_count) SELECT work_item_id,id,'invalid',1,7,1 FROM public.processing_jobs WHERE work_item_id=$1",
	} {
		if _, err := fixture.Pool.Exec(ctx, query, fixture.WorkItemID); err == nil {
			t.Fatalf("partial success accepted: %s", query)
		}
	}
}
