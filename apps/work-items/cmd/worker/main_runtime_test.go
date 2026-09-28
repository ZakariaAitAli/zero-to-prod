package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type stubWorkerRuntimeStore struct {
	closeCalls atomic.Int32
}

func (store *stubWorkerRuntimeStore) GetProcessingJob(
	_ context.Context,
	jobID int64,
	workItemID int64,
) (workerProcessingJob, error) {
	return workerProcessingJob{
		ID:         jobID,
		WorkItemID: workItemID,
		State:      "accepted",
	}, nil
}

func (store *stubWorkerRuntimeStore) CompleteProcessingJob(
	context.Context,
	int64,
	int64,
) (processingCompletion, error) {
	return processingCompletion{}, nil
}

func (store *stubWorkerRuntimeStore) RecordProcessingFailure(
	_ context.Context,
	jobID int64,
	workItemID int64,
	_ string,
	_ int,
) (processingFailure, error) {
	return processingFailure{
		Disposition: processingFailureRetryable,
		Job: workerProcessingJob{
			ID:           jobID,
			WorkItemID:   workItemID,
			State:        "accepted",
			AttemptCount: 1,
		},
	}, nil
}

func (store *stubWorkerRuntimeStore) Close() {
	store.closeCalls.Add(1)
}

type stubWorkerRuntimeSession struct {
	deliveries <-chan amqp.Delivery
	closeCalls atomic.Int32
}

func (session *stubWorkerRuntimeSession) Deliveries() <-chan amqp.Delivery {
	return session.deliveries
}

func (session *stubWorkerRuntimeSession) Close() error {
	session.closeCalls.Add(1)
	return nil
}

func TestRunWorkerProcessPropagatesStoreInitializationFailure(
	t *testing.T,
) {
	storeErr := errors.New(
		"invalid worker PostgreSQL configuration",
	)

	var sessionCalls atomic.Int32
	var reports atomic.Int32

	storeFactory := func(
		_ context.Context,
		databaseURL string,
	) (
		workerRuntimeStore,
		error,
	) {
		if databaseURL != "postgres://worker.example/database" {
			t.Fatalf(
				"unexpected database URL %q",
				databaseURL,
			)
		}

		return nil, storeErr
	}

	sessionFactory := func(
		context.Context,
		string,
		string,
	) (
		workerDeliverySession,
		error,
	) {
		sessionCalls.Add(1)

		return nil, nil
	}

	reporter := func(error) {
		reports.Add(1)
	}

	err := runWorkerProcess(
		context.Background(),
		workerConfig{
			DatabaseURL:       "postgres://worker.example/database",
			RabbitMQWorkerURL: "amqp://worker.example/vhost",
			QueueName:         "work_item_processing",
		},
		storeFactory,
		sessionFactory,
		reporter,
	)

	if !errors.Is(err, storeErr) {
		t.Fatalf(
			"expected store initialization error %v, got %v",
			storeErr,
			err,
		)
	}

	if sessionCalls.Load() != 0 {
		t.Fatalf(
			"session factory called despite store failure: %d",
			sessionCalls.Load(),
		)
	}

	if reports.Load() != 0 {
		t.Fatalf(
			"store initialization failure was reported as session exit: %d",
			reports.Load(),
		)
	}
}

func TestRunWorkerProcessWiresRuntimeAndClosesResourcesOnCancellation(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	store := &stubWorkerRuntimeStore{}
	session := &stubWorkerRuntimeSession{}

	sessionStarted := make(
		chan struct{},
	)

	var storeCalls atomic.Int32
	var sessionCalls atomic.Int32
	var reports atomic.Int32

	storeFactory := func(
		storeContext context.Context,
		databaseURL string,
	) (
		workerRuntimeStore,
		error,
	) {
		storeCalls.Add(1)

		if storeContext != ctx {
			t.Fatal(
				"store factory received different process context",
			)
		}

		if databaseURL != "postgres://worker.example/database" {
			t.Fatalf(
				"unexpected database URL %q",
				databaseURL,
			)
		}

		return store, nil
	}

	sessionFactory := func(
		sessionContext context.Context,
		connectionURL string,
		queueName string,
	) (
		workerDeliverySession,
		error,
	) {
		sessionCalls.Add(1)

		if sessionContext != ctx {
			t.Fatal(
				"session factory received different process context",
			)
		}

		if connectionURL != "amqp://worker.example/vhost" {
			t.Fatalf(
				"unexpected RabbitMQ URL %q",
				connectionURL,
			)
		}

		if queueName != "work_item_processing" {
			t.Fatalf(
				"unexpected RabbitMQ queue %q",
				queueName,
			)
		}

		select {
		case <-sessionStarted:
		default:
			close(sessionStarted)
		}

		return session, nil
	}

	reporter := func(error) {
		reports.Add(1)
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerProcess(
			ctx,
			workerConfig{
				DatabaseURL:       "postgres://worker.example/database",
				RabbitMQWorkerURL: "amqp://worker.example/vhost",
				QueueName:         "work_item_processing",
			},
			storeFactory,
			sessionFactory,
			reporter,
		)
	}()

	select {
	case <-sessionStarted:
	case <-time.After(time.Second):
		t.Fatal(
			"worker process never created RabbitMQ session",
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean worker shutdown, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker process did not stop after cancellation",
		)
	}

	if storeCalls.Load() != 1 {
		t.Fatalf(
			"expected one store construction, got %d",
			storeCalls.Load(),
		)
	}

	if store.closeCalls.Load() != 1 {
		t.Fatalf(
			"expected PostgreSQL store close once, got %d",
			store.closeCalls.Load(),
		)
	}

	if sessionCalls.Load() != 1 {
		t.Fatalf(
			"expected one RabbitMQ session, got %d",
			sessionCalls.Load(),
		)
	}

	if session.closeCalls.Load() != 1 {
		t.Fatalf(
			"expected RabbitMQ session close once, got %d",
			session.closeCalls.Load(),
		)
	}

	if reports.Load() != 0 {
		t.Fatalf(
			"normal shutdown produced %d retry reports",
			reports.Load(),
		)
	}
}
