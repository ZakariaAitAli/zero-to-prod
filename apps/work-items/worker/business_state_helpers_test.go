package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"
)

// businessJob and businessResult are sanitized snapshots used as test and
// experiment evidence. They contain no credentials or connection details.
type businessJob struct {
	ID           int64   `json:"id"`
	State        string  `json:"state"`
	AttemptCount int     `json:"attempt_count"`
	LastError    *string `json:"last_error_code"`
	Finished     bool    `json:"finished"`
}

type businessResult struct {
	ProcessingJobID int64  `json:"processing_job_id"`
	InputTitle      string `json:"input_title"`
	AnalysisVersion int    `json:"analysis_version"`
	CharacterCount  int    `json:"character_count"`
	WordCount       int    `json:"word_count"`
}

type businessState struct {
	WorkItemID int64            `json:"work_item_id"`
	Title      string           `json:"title"`
	Status     string           `json:"status"`
	Jobs       []businessJob    `json:"jobs"`
	Results    []businessResult `json:"results"`
}

type businessStateQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readBusinessState(
	ctx context.Context,
	db businessStateQuerier,
	workItemID int64,
) (businessState, error) {
	state := businessState{WorkItemID: workItemID, Jobs: []businessJob{}, Results: []businessResult{}}
	if err := db.QueryRow(ctx, "SELECT title, status FROM public.work_items WHERE id=$1", workItemID).
		Scan(&state.Title, &state.Status); err != nil {
		return businessState{}, fmt.Errorf("read work item %d: %w", workItemID, err)
	}
	jobs, err := db.Query(ctx, `SELECT id, state, attempt_count, last_error_code, finished_at IS NOT NULL
 FROM public.processing_jobs WHERE work_item_id=$1 ORDER BY id`, workItemID)
	if err != nil {
		return businessState{}, fmt.Errorf("read jobs: %w", err)
	}
	for jobs.Next() {
		var job businessJob
		if err := jobs.Scan(&job.ID, &job.State, &job.AttemptCount, &job.LastError, &job.Finished); err != nil {
			jobs.Close()
			return businessState{}, err
		}
		state.Jobs = append(state.Jobs, job)
	}
	if err := jobs.Err(); err != nil {
		return businessState{}, err
	}
	results, err := db.Query(ctx, `SELECT processing_job_id, input_title, analysis_version, character_count, word_count
 FROM public.work_item_results WHERE work_item_id=$1`, workItemID)
	if err != nil {
		return businessState{}, fmt.Errorf("read results: %w", err)
	}
	for results.Next() {
		var result businessResult
		if err := results.Scan(&result.ProcessingJobID, &result.InputTitle, &result.AnalysisVersion,
			&result.CharacterCount, &result.WordCount); err != nil {
			results.Close()
			return businessState{}, err
		}
		state.Results = append(state.Results, result)
	}
	return state, results.Err()
}

func (state businessState) job(id int64) (businessJob, bool) {
	for _, job := range state.Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return businessJob{}, false
}

func (state businessState) String() string {
	encoded, _ := json.Marshal(state)
	return string(encoded)
}

// successInvariantViolation checks ADR 0003's committed success invariant:
// done <=> one result <=> the result references the single succeeded job, and
// at most one accepted-or-succeeded job exists.
func successInvariantViolation(state businessState) error {
	var active, succeeded []businessJob
	for _, job := range state.Jobs {
		switch job.State {
		case "accepted":
			active = append(active, job)
		case "succeeded":
			succeeded = append(succeeded, job)
		}
	}
	if len(active)+len(succeeded) > 1 {
		return fmt.Errorf("more than one accepted/succeeded job: %s", state)
	}
	switch state.Status {
	case "done":
		if len(state.Results) != 1 || len(succeeded) != 1 || state.Results[0].ProcessingJobID != succeeded[0].ID {
			return fmt.Errorf("done item without exactly one result from its succeeded job: %s", state)
		}
		characters, words := analyzeTitle(state.Title)
		result := state.Results[0]
		if result.InputTitle != state.Title || result.AnalysisVersion != 1 ||
			result.CharacterCount != characters || result.WordCount != words {
			return fmt.Errorf("result does not match deterministic title analysis: %s", state)
		}
	case "pending":
		if len(state.Results) != 0 || len(succeeded) != 0 {
			return fmt.Errorf("pending item has partial success: %s", state)
		}
	default:
		return fmt.Errorf("unexpected item status: %s", state)
	}
	return nil
}

func requireBusinessState(
	t *testing.T,
	ctx context.Context,
	db businessStateQuerier,
	workItemID int64,
) businessState {
	t.Helper()
	state, err := readBusinessState(ctx, db, workItemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := successInvariantViolation(state); err != nil {
		t.Fatal(err)
	}
	return state
}

// requireCompletedOnce asserts the item is done, owned by jobID, and that the
// job recorded exactly one durable attempt.
func requireCompletedOnce(t *testing.T, state businessState, jobID int64) {
	t.Helper()
	job, ok := state.job(jobID)
	if state.Status != "done" || !ok || job.State != "succeeded" || job.AttemptCount != 1 ||
		!job.Finished || len(state.Results) != 1 || state.Results[0].ProcessingJobID != jobID {
		t.Fatalf("expected one completion by job %d: %s", jobID, state)
	}
}

// requireUntouched asserts no durable processing outcome was recorded.
func requireUntouched(t *testing.T, state businessState, jobID int64) {
	t.Helper()
	job, ok := state.job(jobID)
	if state.Status != "pending" || !ok || job.State != "accepted" || job.AttemptCount != 0 ||
		job.Finished || len(state.Results) != 0 {
		t.Fatalf("expected untouched pending item and accepted job %d: %s", jobID, state)
	}
}

// publishFixturePayload publishes one persistent message with publisher
// confirms using the fixture (administrative) RabbitMQ identity.
func publishFixturePayload(
	ctx context.Context,
	fixtureURL string,
	queueName string,
	payload []byte,
) error {
	connection, err := amqp.Dial(fixtureURL)
	if err != nil {
		return fmt.Errorf("connect RabbitMQ fixture identity: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ fixture channel: %w", err)
	}
	defer channel.Close()
	if err := channel.Confirm(false); err != nil {
		return fmt.Errorf("enable publisher confirms: %w", err)
	}
	confirmation, err := channel.PublishWithDeferredConfirmWithContext(ctx, "", queueName, true, false,
		amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Type: workItemProcessMessageType, Body: payload})
	if err != nil {
		return fmt.Errorf("publish fixture payload: %w", err)
	}
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait for publisher confirm: %w", err)
	}
	if !acked {
		return fmt.Errorf("RabbitMQ negatively acknowledged fixture payload")
	}
	return nil
}

// inspectQueue passively inspects the queue. Messages counts ready messages
// only; unacknowledged deliveries held by a consumer are not included.
func inspectQueue(fixtureURL, queueName string) (amqp.Queue, error) {
	connection, err := amqp.Dial(fixtureURL)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("connect RabbitMQ fixture identity: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("open RabbitMQ fixture channel: %w", err)
	}
	defer channel.Close()
	return channel.QueueDeclarePassive(queueName, true, false, false, false, nil)
}

func workerPayload(jobID, workItemID int64) []byte {
	return []byte(fmt.Sprintf(`{"type":"work_item.process","version":1,"job_id":%d,"work_item_id":%d}`, jobID, workItemID))
}
