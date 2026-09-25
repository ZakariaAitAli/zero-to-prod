package main

import (
	"context"
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

type workerRuntimeStore interface {
	processingJobCompleter
	Close()
}

type workerRuntimeStoreFactory func(
	context.Context,
	string,
) (
	workerRuntimeStore,
	error,
)

type workerRuntimeSessionFactory func(
	context.Context,
	string,
	string,
) (
	workerDeliverySession,
	error,
)

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

func runWorkerProcess(
	ctx context.Context,
	config workerConfig,
	newStore workerRuntimeStoreFactory,
	newSession workerRuntimeSessionFactory,
	reportExit workerSessionExitReporter,
) error {
	store, err := newStore(
		ctx,
		config.DatabaseURL,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize worker PostgreSQL store: %w",
			err,
		)
	}
	defer store.Close()

	sessionFactory := func(
		sessionContext context.Context,
	) (
		workerDeliverySession,
		error,
	) {
		return newSession(
			sessionContext,
			config.RabbitMQWorkerURL,
			config.QueueName,
		)
	}

	runSession := func(
		sessionContext context.Context,
	) error {
		return runWorkerSession(
			sessionContext,
			store,
			sessionFactory,
		)
	}

	return runWorkerLifecycleWithReporter(
		ctx,
		runSession,
		workerSessionRetryDelay,
		reportExit,
	)
}

func main() {}
