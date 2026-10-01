package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRabbitMQWorkerSessionIntegrationConnectsWithWorkerIdentity(
	t *testing.T,
) {
	connectionURL := os.Getenv(
		"RABBITMQ_WORKER_URL",
	)
	if connectionURL == "" {
		t.Skip(
			"RABBITMQ_WORKER_URL not set",
		)
	}

	queueName := os.Getenv(
		"RABBITMQ_QUEUE",
	)
	if queueName == "" {
		queueName = "work_item_processing"
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)

	session, err := newRabbitMQWorkerSession(
		ctx,
		connectionURL,
		queueName,
	)
	if err != nil {
		cancel()

		t.Fatalf(
			"open real RabbitMQ worker session: %v",
			err,
		)
	}

	if session.Deliveries() == nil {
		cancel()
		_ = session.Close()

		t.Fatal(
			"real RabbitMQ worker session returned nil deliveries",
		)
	}

	// ConsumeWithContext owns consumer cancellation. Cancel before closing
	// the channel and connection so any unacknowledged delivery remains
	// recoverable by RabbitMQ rather than being treated as processed.
	cancel()

	if err := session.Close(); err != nil {
		t.Fatalf(
			"close real RabbitMQ worker session: %v",
			err,
		)
	}
}
