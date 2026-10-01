package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestAcceptedProcessingSurvivesPostCommitPublisherGap(
	t *testing.T,
) {
	databaseURL := os.Getenv(
		"OUTBOX_INTEGRATION_DATABASE_URL",
	)
	rabbitMQURL := os.Getenv(
		"RABBITMQ_PUBLISHER_URL",
	)
	queueName := os.Getenv(
		"RABBITMQ_QUEUE",
	)

	if databaseURL == "" ||
		rabbitMQURL == "" ||
		queueName == "" {
		t.Skip(
			"OUTBOX_INTEGRATION_DATABASE_URL, RABBITMQ_PUBLISHER_URL, and RABBITMQ_QUEUE are required",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	firstStore, err := newPostgresStore(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open first API PostgreSQL store: %v",
			err,
		)
	}

	var preexistingUnpublished int

	err = firstStore.pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM public.outbox_messages
			WHERE published_at IS NULL
		`,
	).Scan(
		&preexistingUnpublished,
	)
	if err != nil {
		firstStore.Close()

		t.Fatalf(
			"inspect preexisting unpublished outbox: %v",
			err,
		)
	}

	if preexistingUnpublished != 0 {
		firstStore.Close()

		t.Fatalf(
			"integration precondition failed: expected no unpublished outbox rows, found %d",
			preexistingUnpublished,
		)
	}

	item, err := firstStore.CreateWorkItem(
		ctx,
		"issue-103 post-commit pre-publish recovery",
		workItemStatusPending,
	)
	if err != nil {
		firstStore.Close()

		t.Fatalf(
			"create integration Work Item: %v",
			err,
		)
	}

	readiness := &readinessState{}
	readiness.set(true)

	handler := newHandler(
		"issue-103-integration",
		readiness,
		firstStore,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf(
			"/items/%d/process",
			item.ID,
		),
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusAccepted {
		firstStore.Close()

		t.Fatalf(
			"expected HTTP %d acceptance, got %d body=%s",
			http.StatusAccepted,
			response.Code,
			response.Body.String(),
		)
	}

	var acceptedJob processingJob

	if err := json.NewDecoder(
		response.Body,
	).Decode(
		&acceptedJob,
	); err != nil {
		firstStore.Close()

		t.Fatalf(
			"decode accepted processing job: %v",
			err,
		)
	}

	if acceptedJob.WorkItemID != item.ID {
		firstStore.Close()

		t.Fatalf(
			"accepted job references work_item_id=%d, expected %d",
			acceptedJob.WorkItemID,
			item.ID,
		)
	}

	var (
		outboxID        int64
		publishAttempts int
		published       bool
	)

	err = firstStore.pool.QueryRow(
		ctx,
		`
			SELECT
				id,
				publish_attempts,
				published_at IS NOT NULL
			FROM public.outbox_messages
			WHERE processing_job_id = $1
		`,
		acceptedJob.ID,
	).Scan(
		&outboxID,
		&publishAttempts,
		&published,
	)
	if err != nil {
		firstStore.Close()

		t.Fatalf(
			"inspect committed acceptance outbox: %v",
			err,
		)
	}

	if publishAttempts != 0 {
		firstStore.Close()

		t.Fatalf(
			"publisher unexpectedly ran before simulated API failure: attempts=%d",
			publishAttempts,
		)
	}

	if published {
		firstStore.Close()

		t.Fatal(
			"outbox row unexpectedly published before simulated API failure",
		)
	}

	// Simulate the API process disappearing after HTTP 202 and transaction
	// commit, but before its publisher service had any opportunity to run.
	firstStore.Close()

	// A fresh store represents a restarted API process. No client request is
	// repeated here: recovery must come entirely from durable PostgreSQL state.
	restartedStore, err := newPostgresStore(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open restarted API PostgreSQL store: %v",
			err,
		)
	}
	defer restartedStore.Close()

	var (
		recoveredJobState       string
		recoveredOutboxID       int64
		recoveredAttempts       int
		recoveredPublished      bool
		recoveredWorkItemStatus string
	)

	err = restartedStore.pool.QueryRow(
		ctx,
		`
			SELECT
				wi.status,
				pj.state,
				om.id,
				om.publish_attempts,
				om.published_at IS NOT NULL
			FROM public.processing_jobs AS pj
			JOIN public.work_items AS wi
			  ON wi.id = pj.work_item_id
			JOIN public.outbox_messages AS om
			  ON om.processing_job_id = pj.id
			WHERE pj.id = $1
		`,
		acceptedJob.ID,
	).Scan(
		&recoveredWorkItemStatus,
		&recoveredJobState,
		&recoveredOutboxID,
		&recoveredAttempts,
		&recoveredPublished,
	)
	if err != nil {
		t.Fatalf(
			"recover committed acceptance after restart: %v",
			err,
		)
	}

	if recoveredOutboxID != outboxID {
		t.Fatalf(
			"restarted process observed different outbox id: before=%d after=%d",
			outboxID,
			recoveredOutboxID,
		)
	}

	if recoveredJobState != "accepted" {
		t.Fatalf(
			"restarted process observed job state %q",
			recoveredJobState,
		)
	}

	if recoveredWorkItemStatus != workItemStatusPending {
		t.Fatalf(
			"restart changed Work Item status to %q",
			recoveredWorkItemStatus,
		)
	}

	if recoveredAttempts != 0 ||
		recoveredPublished {
		t.Fatalf(
			"durable outbox changed while API was absent: attempts=%d published=%t",
			recoveredAttempts,
			recoveredPublished,
		)
	}

	publisher, err := newRabbitMQPublisher(
		rabbitMQURL,
		queueName,
	)
	if err != nil {
		t.Fatalf(
			"open restarted RabbitMQ publisher: %v",
			err,
		)
	}

	processed, publishErr := publishNextOutboxMessage(
		ctx,
		restartedStore,
		publisher,
	)

	closeErr := publisher.Close()

	if publishErr != nil {
		t.Fatalf(
			"publish recovered outbox message: %v",
			publishErr,
		)
	}

	if closeErr != nil {
		t.Fatalf(
			"close recovered RabbitMQ publisher: %v",
			closeErr,
		)
	}

	if !processed {
		t.Fatal(
			"restarted publisher did not discover accepted durable work",
		)
	}

	var (
		finalAttempts  int
		finalPublished bool
	)

	err = restartedStore.pool.QueryRow(
		ctx,
		`
			SELECT
				publish_attempts,
				published_at IS NOT NULL
			FROM public.outbox_messages
			WHERE id = $1
		`,
		outboxID,
	).Scan(
		&finalAttempts,
		&finalPublished,
	)
	if err != nil {
		t.Fatalf(
			"inspect recovered publication state: %v",
			err,
		)
	}

	if finalAttempts != 1 {
		t.Fatalf(
			"expected one publication attempt after restart, got %d",
			finalAttempts,
		)
	}

	if !finalPublished {
		t.Fatal(
			"restarted publisher did not durably mark confirmed publication",
		)
	}

	t.Logf(
		"work_item_id=%d job_id=%d outbox_id=%d attempts=%d published=%t",
		item.ID,
		acceptedJob.ID,
		outboxID,
		finalAttempts,
		finalPublished,
	)
}
