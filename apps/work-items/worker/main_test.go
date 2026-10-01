package main

import (
	"strings"
	"testing"
)

func environmentFrom(
	values map[string]string,
) environmentLookup {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadWorkerConfigRequiresDatabaseURL(
	t *testing.T,
) {
	_, err := loadWorkerConfig(
		environmentFrom(
			map[string]string{
				"RABBITMQ_WORKER_URL": "amqp://worker.example/vhost",
			},
		),
	)

	if err == nil {
		t.Fatal(
			"missing DATABASE_URL unexpectedly accepted",
		)
	}

	if !strings.Contains(
		err.Error(),
		"DATABASE_URL is required",
	) {
		t.Fatalf(
			"expected DATABASE_URL requirement, got %v",
			err,
		)
	}
}

func TestLoadWorkerConfigRequiresRabbitMQWorkerURL(
	t *testing.T,
) {
	_, err := loadWorkerConfig(
		environmentFrom(
			map[string]string{
				"DATABASE_URL": "postgres://worker.example/database",
			},
		),
	)

	if err == nil {
		t.Fatal(
			"missing RABBITMQ_WORKER_URL unexpectedly accepted",
		)
	}

	if !strings.Contains(
		err.Error(),
		"RABBITMQ_WORKER_URL is required",
	) {
		t.Fatalf(
			"expected RABBITMQ_WORKER_URL requirement, got %v",
			err,
		)
	}
}

func TestLoadWorkerConfigUsesDefaultQueue(
	t *testing.T,
) {
	config, err := loadWorkerConfig(
		environmentFrom(
			map[string]string{
				"DATABASE_URL":        "postgres://worker.example/database",
				"RABBITMQ_WORKER_URL": "amqp://worker.example/vhost",
			},
		),
	)
	if err != nil {
		t.Fatalf(
			"load worker configuration: %v",
			err,
		)
	}

	if config.DatabaseURL != "postgres://worker.example/database" {
		t.Fatalf(
			"unexpected database URL %q",
			config.DatabaseURL,
		)
	}

	if config.RabbitMQWorkerURL != "amqp://worker.example/vhost" {
		t.Fatalf(
			"unexpected RabbitMQ worker URL %q",
			config.RabbitMQWorkerURL,
		)
	}

	if config.QueueName != defaultWorkerQueue {
		t.Fatalf(
			"expected default queue %q, got %q",
			defaultWorkerQueue,
			config.QueueName,
		)
	}
}

func TestLoadWorkerConfigPreservesExplicitQueue(
	t *testing.T,
) {
	config, err := loadWorkerConfig(
		environmentFrom(
			map[string]string{
				"DATABASE_URL":        "postgres://worker.example/database",
				"RABBITMQ_WORKER_URL": "amqp://worker.example/vhost",
				"RABBITMQ_QUEUE":      "custom_processing_queue",
			},
		),
	)
	if err != nil {
		t.Fatalf(
			"load worker configuration: %v",
			err,
		)
	}

	if config.QueueName != "custom_processing_queue" {
		t.Fatalf(
			"expected explicit queue, got %q",
			config.QueueName,
		)
	}
}
