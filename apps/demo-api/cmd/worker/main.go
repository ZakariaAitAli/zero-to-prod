package main

import (
	"fmt"
	"time"
)

const (
	defaultWorkerQueue      = "work_item_processing"
	workerSessionRetryDelay = 1 * time.Second
)

type workerConfig struct {
	DatabaseURL       string
	RabbitMQWorkerURL string
	QueueName         string
}

type environmentLookup func(string) string

func loadWorkerConfig(
	lookup environmentLookup,
) (workerConfig, error) {
	databaseURL := lookup("DATABASE_URL")
	if databaseURL == "" {
		return workerConfig{}, fmt.Errorf(
			"DATABASE_URL is required",
		)
	}

	rabbitMQWorkerURL := lookup(
		"RABBITMQ_WORKER_URL",
	)
	if rabbitMQWorkerURL == "" {
		return workerConfig{}, fmt.Errorf(
			"RABBITMQ_WORKER_URL is required",
		)
	}

	queueName := lookup("RABBITMQ_QUEUE")
	if queueName == "" {
		queueName = defaultWorkerQueue
	}

	return workerConfig{
		DatabaseURL:       databaseURL,
		RabbitMQWorkerURL: rabbitMQWorkerURL,
		QueueName:         queueName,
	}, nil
}

func main() {}
