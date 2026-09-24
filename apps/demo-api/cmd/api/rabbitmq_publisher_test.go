package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func rabbitMQIntegrationURL(t *testing.T) string {
	t.Helper()

	connectionURL := os.Getenv("RABBITMQ_PUBLISHER_URL")
	if connectionURL == "" {
		t.Skip("RABBITMQ_PUBLISHER_URL not set")
	}

	return connectionURL
}

func TestRabbitMQPublisherIntegrationPublishesConfirmedMessage(
	t *testing.T,
) {
	connectionURL := rabbitMQIntegrationURL(t)

	queueName := os.Getenv("RABBITMQ_QUEUE")
	if queueName == "" {
		queueName = "work_item_processing"
	}

	publisher, err := newRabbitMQPublisher(
		connectionURL,
		queueName,
	)
	if err != nil {
		t.Fatalf("initialize RabbitMQ publisher: %v", err)
	}
	defer func() {
		if err := publisher.Close(); err != nil {
			t.Errorf("close RabbitMQ publisher: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	message := outboxMessage{
		ID:              10301,
		ProcessingJobID: 10302,
		EventType:       "work_item.process",
		Payload: []byte(
			`{"type":"work_item.process","version":1,"job_id":10302,"work_item_id":10303}`,
		),
		PublishAttempts: 1,
	}

	if err := publisher.Publish(ctx, message); err != nil {
		t.Fatalf("publish confirmed RabbitMQ message: %v", err)
	}
}

func TestRabbitMQPublisherIntegrationRejectsUnroutableMessage(
	t *testing.T,
) {
	connectionURL := rabbitMQIntegrationURL(t)

	publisher, err := newRabbitMQPublisher(
		connectionURL,
		"issue103-queue-that-does-not-exist",
	)
	if err != nil {
		t.Fatalf("initialize RabbitMQ publisher: %v", err)
	}
	defer func() {
		if err := publisher.Close(); err != nil {
			t.Errorf("close RabbitMQ publisher: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	err = publisher.Publish(
		ctx,
		outboxMessage{
			ID:              10311,
			ProcessingJobID: 10312,
			EventType:       "work_item.process",
			Payload:         []byte(`{"job_id":10312}`),
			PublishAttempts: 1,
		},
	)
	if err == nil {
		t.Fatal("expected unroutable mandatory publication to fail")
	}

	if !errors.Is(err, errRabbitMQPublishReturned) {
		t.Fatalf(
			"expected RabbitMQ return error, got: %v",
			err,
		)
	}
}
