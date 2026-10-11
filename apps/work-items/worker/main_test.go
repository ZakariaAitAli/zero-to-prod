package main

import (
	"os"
	"path/filepath"
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

func TestLoadWorkerConfigReadsConnectionURLFiles(
	t *testing.T,
) {
	directory := t.TempDir()
	databaseURLPath := filepath.Join(directory, "database-url")
	rabbitMQURLPath := filepath.Join(directory, "rabbitmq-url")

	if err := os.WriteFile(
		databaseURLPath,
		[]byte("postgres://worker.example/database\n"),
		0o600,
	); err != nil {
		t.Fatalf("write database URL file: %v", err)
	}

	if err := os.WriteFile(
		rabbitMQURLPath,
		[]byte("amqp://worker.example/vhost\n"),
		0o600,
	); err != nil {
		t.Fatalf("write RabbitMQ URL file: %v", err)
	}

	config, err := loadWorkerConfig(
		environmentFrom(
			map[string]string{
				"DATABASE_URL_FILE":        databaseURLPath,
				"RABBITMQ_WORKER_URL_FILE": rabbitMQURLPath,
			},
		),
	)
	if err != nil {
		t.Fatalf(
			"load worker configuration: %v",
			err,
		)
	}

	if config.DatabaseURL != "postgres://worker.example/database" ||
		config.RabbitMQWorkerURL != "amqp://worker.example/vhost" {
		t.Fatalf(
			"unexpected connection URLs %q and %q",
			config.DatabaseURL,
			config.RabbitMQWorkerURL,
		)
	}
}
