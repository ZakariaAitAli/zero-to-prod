package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleOutboxStore struct {
	mu sync.Mutex

	message outboxMessage

	published       bool
	publishAttempts int
	failureCodes    []string

	markedPublished chan struct{}
	markOnce        sync.Once
}

func newLifecycleOutboxStore(
	message outboxMessage,
) *lifecycleOutboxStore {
	return &lifecycleOutboxStore{
		message:         message,
		markedPublished: make(chan struct{}),
	}
}

func (store *lifecycleOutboxStore) BeginOutboxPublishAttempt(
	context.Context,
) (outboxMessage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if store.published {
		return outboxMessage{}, errNoOutboxMessage
	}

	store.publishAttempts++

	message := store.message
	message.PublishAttempts = store.publishAttempts

	return message, nil
}

func (store *lifecycleOutboxStore) MarkOutboxPublished(
	_ context.Context,
	messageID int64,
) error {
	store.mu.Lock()

	if messageID != store.message.ID {
		store.mu.Unlock()

		return errors.New("unexpected outbox message id")
	}

	store.published = true
	store.mu.Unlock()

	store.markOnce.Do(func() {
		close(store.markedPublished)
	})

	return nil
}

func (store *lifecycleOutboxStore) RecordOutboxPublishFailure(
	_ context.Context,
	messageID int64,
	code string,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	if messageID != store.message.ID {
		return errors.New("unexpected outbox message id")
	}

	store.failureCodes = append(
		store.failureCodes,
		code,
	)

	return nil
}

func (store *lifecycleOutboxStore) snapshot() (
	bool,
	int,
	[]string,
) {
	store.mu.Lock()
	defer store.mu.Unlock()

	failureCodes := append(
		[]string(nil),
		store.failureCodes...,
	)

	return store.published, store.publishAttempts, failureCodes
}

type lifecycleBrokerPublisher struct {
	publishErr error

	publishCalls atomic.Int32
	closeCalls   atomic.Int32
}

func (publisher *lifecycleBrokerPublisher) Publish(
	context.Context,
	outboxMessage,
) error {
	publisher.publishCalls.Add(1)

	return publisher.publishErr
}

func (publisher *lifecycleBrokerPublisher) Close() error {
	publisher.closeCalls.Add(1)

	return nil
}

func TestRunOutboxPublisherKeepsRetryingWhenBrokerConnectionFails(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(context.Background())

	store := newLifecycleOutboxStore(
		outboxMessage{
			ID:              101,
			ProcessingJobID: 201,
			EventType:       "work_item.process",
			Payload:         []byte(`{"job_id":201}`),
		},
	)

	firstFactoryCall := make(chan struct{})
	var firstCallOnce sync.Once
	var factoryCalls atomic.Int32

	factory := func() (
		closeableBrokerMessagePublisher,
		error,
	) {
		factoryCalls.Add(1)

		firstCallOnce.Do(func() {
			close(firstFactoryCall)
		})

		return nil, errors.New("RabbitMQ unavailable")
	}

	result := make(chan error, 1)

	go func() {
		result <- runOutboxPublisher(
			ctx,
			store,
			factory,
			10*time.Millisecond,
			10*time.Millisecond,
			100*time.Millisecond,
		)
	}()

	select {
	case <-firstFactoryCall:
	case <-time.After(time.Second):
		t.Fatal("publisher factory was never called")
	}

	// Give the loop enough time to demonstrate that a connection failure
	// does not cause it to exit immediately.
	time.Sleep(30 * time.Millisecond)

	select {
	case err := <-result:
		t.Fatalf(
			"publisher loop exited after broker connection failure: %v",
			err,
		)
	default:
	}

	if factoryCalls.Load() < 2 {
		t.Fatalf(
			"expected broker connection retry, got %d factory calls",
			factoryCalls.Load(),
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected cancellation to stop publisher cleanly, got: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal("publisher loop did not stop after cancellation")
	}

	published, attempts, failures := store.snapshot()

	if published {
		t.Fatal("broker connection failure unexpectedly published work")
	}

	if attempts != 0 {
		t.Fatalf(
			"broker connection failure must not begin a DB publish attempt; got %d",
			attempts,
		)
	}

	if len(failures) != 0 {
		t.Fatalf(
			"broker connection failure unexpectedly recorded message failure: %#v",
			failures,
		)
	}
}

func TestRunOutboxPublisherReconnectsAndRetriesAfterPublishFailure(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newLifecycleOutboxStore(
		outboxMessage{
			ID:              301,
			ProcessingJobID: 401,
			EventType:       "work_item.process",
			Payload:         []byte(`{"job_id":401}`),
		},
	)

	failedPublisher := &lifecycleBrokerPublisher{
		publishErr: errors.New("connection lost"),
	}

	successfulPublisher := &lifecycleBrokerPublisher{}

	var factoryCalls atomic.Int32

	factory := func() (
		closeableBrokerMessagePublisher,
		error,
	) {
		call := factoryCalls.Add(1)

		switch call {
		case 1:
			return failedPublisher, nil

		default:
			return successfulPublisher, nil
		}
	}

	result := make(chan error, 1)

	go func() {
		result <- runOutboxPublisher(
			ctx,
			store,
			factory,
			10*time.Millisecond,
			10*time.Millisecond,
			100*time.Millisecond,
		)
	}()

	select {
	case <-store.markedPublished:
	case <-time.After(time.Second):
		t.Fatal("publisher never recovered and marked the outbox row published")
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected publisher shutdown to succeed, got: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal("publisher loop did not stop after cancellation")
	}

	published, attempts, failures := store.snapshot()

	if !published {
		t.Fatal("expected outbox row to be published after reconnect")
	}

	if attempts != 2 {
		t.Fatalf(
			"expected exactly two durable publish attempts, got %d",
			attempts,
		)
	}

	if len(failures) != 1 {
		t.Fatalf(
			"expected one recorded broker failure, got %#v",
			failures,
		)
	}

	if failures[0] != outboxPublishFailureCode {
		t.Fatalf(
			"expected failure code %q, got %q",
			outboxPublishFailureCode,
			failures[0],
		)
	}

	if factoryCalls.Load() < 2 {
		t.Fatalf(
			"expected publisher reconnection, got %d factory calls",
			factoryCalls.Load(),
		)
	}

	if failedPublisher.publishCalls.Load() != 1 {
		t.Fatalf(
			"expected failed publisher to receive one publish, got %d",
			failedPublisher.publishCalls.Load(),
		)
	}

	if failedPublisher.closeCalls.Load() != 1 {
		t.Fatalf(
			"expected failed publisher to be closed once, got %d",
			failedPublisher.closeCalls.Load(),
		)
	}

	if successfulPublisher.publishCalls.Load() != 1 {
		t.Fatalf(
			"expected replacement publisher to publish once, got %d",
			successfulPublisher.publishCalls.Load(),
		)
	}

	if successfulPublisher.closeCalls.Load() != 1 {
		t.Fatalf(
			"expected active publisher to close during shutdown, got %d",
			successfulPublisher.closeCalls.Load(),
		)
	}
}
