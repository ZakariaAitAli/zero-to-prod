package main

import (
	"context"
	"testing"
	"time"
)

func TestWorkerRestartBeforeCompletionRecoversOutstandingDelivery(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	fixture := requireWorkerRabbitMQIntegrationFixture(
		t,
		ctx,
	)

	store := fixture.Store
	jobID := fixture.JobID
	workItemID := fixture.WorkItemID
	rabbitMQURL := fixture.WorkerURL
	queueName := fixture.QueueName

	firstSession, err := newRabbitMQWorkerSession(
		ctx,
		rabbitMQURL,
		queueName,
	)
	if err != nil {
		t.Fatalf(
			"open first worker session: %v",
			err,
		)
	}

	var firstPayload []byte

	select {
	case delivery, ok := <-firstSession.Deliveries():
		if !ok {
			_ = firstSession.Close()

			t.Fatal(
				"first worker delivery stream closed unexpectedly",
			)
		}

		if delivery.Redelivered {
			_ = firstSession.Close()

			t.Fatal(
				"expected initial delivery, got redelivery",
			)
		}

		message, err := decodeWorkerMessage(
			delivery.Body,
		)
		if err != nil {
			_ = firstSession.Close()

			t.Fatalf(
				"decode initial delivery: %v",
				err,
			)
		}

		if message.JobID != jobID ||
			message.WorkItemID != workItemID {
			_ = firstSession.Close()

			t.Fatalf(
				"unexpected initial identity: job=%d work_item=%d",
				message.JobID,
				message.WorkItemID,
			)
		}

		firstPayload = append(
			[]byte(nil),
			delivery.Body...,
		)

	case <-ctx.Done():
		_ = firstSession.Close()

		t.Fatalf(
			"timed out waiting for initial delivery: %v",
			ctx.Err(),
		)
	}

	// Simulate worker process loss before the processor runs and before any
	// durable processing completion or failure is recorded. Closing the AMQP
	// connection also means no ACK was sent.
	if err := firstSession.Close(); err != nil {
		t.Fatalf(
			"close crashed worker session: %v",
			err,
		)
	}

	jobBeforeRestart, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect job after simulated crash: %v",
			err,
		)
	}

	if jobBeforeRestart.State != "accepted" {
		t.Fatalf(
			"pre-completion crash changed job state to %q",
			jobBeforeRestart.State,
		)
	}

	if jobBeforeRestart.AttemptCount != 0 {
		t.Fatalf(
			"pre-completion crash invented attempt_count=%d",
			jobBeforeRestart.AttemptCount,
		)
	}

	if jobBeforeRestart.FinishedAt != nil {
		t.Fatal(
			"pre-completion crash unexpectedly set finished_at",
		)
	}

	secondSession, err := newRabbitMQWorkerSession(
		ctx,
		rabbitMQURL,
		queueName,
	)
	if err != nil {
		t.Fatalf(
			"open restarted worker session: %v",
			err,
		)
	}
	defer func() {
		if err := secondSession.Close(); err != nil {
			t.Errorf(
				"close restarted worker session: %v",
				err,
			)
		}
	}()

	select {
	case delivery, ok := <-secondSession.Deliveries():
		if !ok {
			t.Fatal(
				"restarted worker delivery stream closed unexpectedly",
			)
		}

		if !delivery.Redelivered {
			t.Fatal(
				"expected outstanding message to be marked redelivered",
			)
		}

		if string(delivery.Body) != string(firstPayload) {
			t.Fatal(
				"recovered payload differs from original delivery",
			)
		}

		result, err := processRabbitMQDelivery(
			ctx,
			store,
			delivery,
		)
		if err != nil {
			t.Fatalf(
				"process recovered delivery: %v",
				err,
			)
		}

		if result.Settlement != settlementAck {
			t.Fatalf(
				"expected recovered delivery ACK, got %q",
				result.Settlement,
			)
		}

		if result.HandlingErr != nil {
			t.Fatalf(
				"recovered delivery reported handling error: %v",
				result.HandlingErr,
			)
		}

	case <-ctx.Done():
		t.Fatalf(
			"timed out waiting for recovered delivery: %v",
			ctx.Err(),
		)
	}

	finalJob, err := store.GetProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect recovered job: %v",
			err,
		)
	}

	if finalJob.State != "succeeded" {
		t.Fatalf(
			"expected recovered job succeeded, got %q",
			finalJob.State,
		)
	}

	if finalJob.AttemptCount != 1 {
		t.Fatalf(
			"expected one durable processing attempt, got %d",
			finalJob.AttemptCount,
		)
	}

	if finalJob.FinishedAt == nil {
		t.Fatal(
			"recovered job did not record finished_at",
		)
	}
}
