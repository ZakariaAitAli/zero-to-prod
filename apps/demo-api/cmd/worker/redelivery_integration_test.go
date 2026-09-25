package main

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestWorkerRedeliveryIntegrationDoesNotDuplicateCompletion(
	t *testing.T,
) {
	databaseURL := os.Getenv(
		"WORKER_DATABASE_URL",
	)
	rabbitMQURL := os.Getenv(
		"RABBITMQ_WORKER_URL",
	)
	queueName := os.Getenv(
		"RABBITMQ_QUEUE",
	)
	jobIDRaw := os.Getenv(
		"WORKER_TEST_JOB_ID",
	)
	workItemIDRaw := os.Getenv(
		"WORKER_TEST_WORK_ITEM_ID",
	)

	if databaseURL == "" ||
		rabbitMQURL == "" ||
		jobIDRaw == "" ||
		workItemIDRaw == "" {
		t.Skip(
			"WORKER_DATABASE_URL, RABBITMQ_WORKER_URL, WORKER_TEST_JOB_ID, and WORKER_TEST_WORK_ITEM_ID are required",
		)
	}

	if queueName == "" {
		queueName = defaultWorkerQueue
	}

	jobID, err := strconv.ParseInt(
		jobIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf(
			"parse worker test job id: %v",
			err,
		)
	}

	workItemID, err := strconv.ParseInt(
		workItemIDRaw,
		10,
		64,
	)
	if err != nil {
		t.Fatalf(
			"parse worker test Work Item id: %v",
			err,
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	store, err := newPostgresWorkerStore(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open worker PostgreSQL store: %v",
			err,
		)
	}
	defer store.Close()

	firstSession, err := newRabbitMQWorkerSession(
		ctx,
		rabbitMQURL,
		queueName,
	)
	if err != nil {
		t.Fatalf(
			"open first RabbitMQ worker session: %v",
			err,
		)
	}

	var firstPayload []byte

	select {
	case delivery, ok := <-firstSession.Deliveries():
		if !ok {
			_ = firstSession.Close()

			t.Fatal(
				"first RabbitMQ delivery stream closed unexpectedly",
			)
		}

		if delivery.Redelivered {
			_ = firstSession.Close()

			t.Fatal(
				"expected initial delivery, got a redelivery",
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
				"unexpected initial delivery identity: job=%d work_item=%d",
				message.JobID,
				message.WorkItemID,
			)
		}

		settlement, err := handleWorkerMessage(
			ctx,
			store,
			delivery.Body,
		)
		if err != nil {
			_ = firstSession.Close()

			t.Fatalf(
				"complete initial delivery: %v",
				err,
			)
		}

		if settlement != settlementAck {
			_ = firstSession.Close()

			t.Fatalf(
				"expected initial completion to request ACK, got %q",
				settlement,
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

	// Deliberately lose the acknowledgement after the PostgreSQL completion.
	// RabbitMQ must recover the unacknowledged delivery when this session closes.
	if err := firstSession.Close(); err != nil {
		t.Fatalf(
			"close first RabbitMQ session without ACK: %v",
			err,
		)
	}

	secondSession, err := newRabbitMQWorkerSession(
		ctx,
		rabbitMQURL,
		queueName,
	)
	if err != nil {
		t.Fatalf(
			"open second RabbitMQ worker session: %v",
			err,
		)
	}
	defer func() {
		if err := secondSession.Close(); err != nil {
			t.Errorf(
				"close second RabbitMQ worker session: %v",
				err,
			)
		}
	}()

	select {
	case delivery, ok := <-secondSession.Deliveries():
		if !ok {
			t.Fatal(
				"second RabbitMQ delivery stream closed unexpectedly",
			)
		}

		if !delivery.Redelivered {
			t.Fatal(
				"expected RabbitMQ to mark the recovered delivery as redelivered",
			)
		}

		message, err := decodeWorkerMessage(
			delivery.Body,
		)
		if err != nil {
			t.Fatalf(
				"decode redelivered message: %v",
				err,
			)
		}

		if message.JobID != jobID ||
			message.WorkItemID != workItemID {
			t.Fatalf(
				"unexpected redelivery identity: job=%d work_item=%d",
				message.JobID,
				message.WorkItemID,
			)
		}

		if string(delivery.Body) != string(firstPayload) {
			t.Fatal(
				"redelivered payload differs from initial payload",
			)
		}

		result, err := processRabbitMQDelivery(
			ctx,
			store,
			delivery,
		)
		if err != nil {
			t.Fatalf(
				"process and ACK redelivery: %v",
				err,
			)
		}

		if result.Settlement != settlementAck {
			t.Fatalf(
				"expected redelivery ACK settlement, got %q",
				result.Settlement,
			)
		}

		if result.HandlingErr != nil {
			t.Fatalf(
				"terminal redelivery unexpectedly reported handling error: %v",
				result.HandlingErr,
			)
		}

	case <-ctx.Done():
		t.Fatalf(
			"timed out waiting for RabbitMQ redelivery: %v",
			ctx.Err(),
		)
	}

	completion, err := store.CompleteProcessingJob(
		ctx,
		jobID,
		workItemID,
	)
	if err != nil {
		t.Fatalf(
			"inspect completion after redelivery: %v",
			err,
		)
	}

	if completion.Disposition != completionAlreadyTerminal {
		t.Fatalf(
			"expected terminal completion disposition %q, got %q",
			completionAlreadyTerminal,
			completion.Disposition,
		)
	}

	if completion.Job.State != "succeeded" {
		t.Fatalf(
			"expected final processing state succeeded, got %q",
			completion.Job.State,
		)
	}

	if completion.Job.AttemptCount != 1 {
		t.Fatalf(
			"redelivery duplicated durable completion: attempt_count=%d",
			completion.Job.AttemptCount,
		)
	}
}
