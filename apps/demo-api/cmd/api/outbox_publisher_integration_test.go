package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestOutboxPublisherIntegrationRetriesUnroutableThenPublishes(
	t *testing.T,
) {
	databaseURL := os.Getenv("OUTBOX_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OUTBOX_INTEGRATION_DATABASE_URL not set")
	}

	rabbitMQURL := os.Getenv("RABBITMQ_PUBLISHER_URL")
	if rabbitMQURL == "" {
		t.Skip("RABBITMQ_PUBLISHER_URL not set")
	}

	queueName := os.Getenv("RABBITMQ_QUEUE")
	if queueName == "" {
		queueName = "work_item_processing"
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	store, err := newPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("initialize PostgreSQL store: %v", err)
	}
	defer store.Close()

	item, err := store.CreateWorkItem(
		ctx,
		"issue-103 full outbox publisher integration",
		workItemStatusPending,
	)
	if err != nil {
		t.Fatalf("create integration Work Item: %v", err)
	}

	job, err := store.AcceptProcessingJob(ctx, item.ID)
	if err != nil {
		t.Fatalf("accept integration processing job: %v", err)
	}

	var (
		outboxID        int64
		publishAttempts int
		lastErrorCode   string
		published       bool
	)

	readOutboxState := func() {
		t.Helper()

		err := store.pool.QueryRow(
			ctx,
			`
				SELECT
					id,
					publish_attempts,
					COALESCE(last_error_code, ''),
					published_at IS NOT NULL
				FROM public.outbox_messages
				WHERE processing_job_id = $1
			`,
			job.ID,
		).Scan(
			&outboxID,
			&publishAttempts,
			&lastErrorCode,
			&published,
		)
		if err != nil {
			t.Fatalf("read integration outbox state: %v", err)
		}
	}

	readOutboxState()

	if publishAttempts != 0 {
		t.Fatalf(
			"expected initial publish_attempts 0, got %d",
			publishAttempts,
		)
	}

	if lastErrorCode != "" {
		t.Fatalf(
			"expected no initial error code, got %q",
			lastErrorCode,
		)
	}

	if published {
		t.Fatal("new outbox row unexpectedly marked published")
	}

	unroutablePublisher, err := newRabbitMQPublisher(
		rabbitMQURL,
		"issue103-queue-that-does-not-exist",
	)
	if err != nil {
		t.Fatalf(
			"initialize unroutable RabbitMQ publisher: %v",
			err,
		)
	}

	processed, publishErr := publishNextOutboxMessage(
		ctx,
		store,
		unroutablePublisher,
	)

	if err := unroutablePublisher.Close(); err != nil {
		t.Fatalf(
			"close unroutable RabbitMQ publisher: %v",
			err,
		)
	}

	if publishErr == nil {
		t.Fatal("expected unroutable publication to fail")
	}

	if !processed {
		t.Fatal("expected unroutable outbox row to count as processed")
	}

	if !errors.Is(
		publishErr,
		errRabbitMQPublishReturned,
	) {
		t.Fatalf(
			"expected RabbitMQ returned-message error, got: %v",
			publishErr,
		)
	}

	readOutboxState()

	if publishAttempts != 1 {
		t.Fatalf(
			"expected publish_attempts 1 after failure, got %d",
			publishAttempts,
		)
	}

	if lastErrorCode != outboxPublishFailureCode {
		t.Fatalf(
			"expected failure code %q, got %q",
			outboxPublishFailureCode,
			lastErrorCode,
		)
	}

	if published {
		t.Fatal("failed broker publish unexpectedly marked row published")
	}

	publisher, err := newRabbitMQPublisher(
		rabbitMQURL,
		queueName,
	)
	if err != nil {
		t.Fatalf("initialize RabbitMQ publisher: %v", err)
	}

	processed, err = publishNextOutboxMessage(
		ctx,
		store,
		publisher,
	)

	if closeErr := publisher.Close(); closeErr != nil {
		t.Fatalf(
			"close RabbitMQ publisher: %v",
			closeErr,
		)
	}

	if err != nil {
		t.Fatalf(
			"retry confirmed RabbitMQ publication: %v",
			err,
		)
	}

	if !processed {
		t.Fatal("expected retry to process the outbox row")
	}

	readOutboxState()

	if publishAttempts != 2 {
		t.Fatalf(
			"expected publish_attempts 2 after retry, got %d",
			publishAttempts,
		)
	}

	if lastErrorCode != "" {
		t.Fatalf(
			"expected success to clear error code, got %q",
			lastErrorCode,
		)
	}

	if !published {
		t.Fatal("confirmed RabbitMQ publication was not marked published")
	}

	var (
		workItemStatus string
		jobState       string
	)

	err = store.pool.QueryRow(
		ctx,
		`
			SELECT wi.status, pj.state
			FROM public.work_items AS wi
			JOIN public.processing_jobs AS pj
			  ON pj.work_item_id = wi.id
			WHERE wi.id = $1
			  AND pj.id = $2
		`,
		item.ID,
		job.ID,
	).Scan(
		&workItemStatus,
		&jobState,
	)
	if err != nil {
		t.Fatalf(
			"read Work Item / processing state: %v",
			err,
		)
	}

	if workItemStatus != workItemStatusPending {
		t.Fatalf(
			"publisher changed Work Item status: %q",
			workItemStatus,
		)
	}

	if jobState != "accepted" {
		t.Fatalf(
			"publisher changed processing job state: %q",
			jobState,
		)
	}

	t.Logf(
		"work_item_id=%d job_id=%d outbox_id=%d attempts=%d published=%t",
		item.ID,
		job.ID,
		outboxID,
		publishAttempts,
		published,
	)
}
