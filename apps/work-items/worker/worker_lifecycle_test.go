package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWorkerLifecycleRetriesAfterSessionFailure(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	firstFailure := make(chan struct{})
	secondSession := make(chan struct{})

	var calls atomic.Int32

	runner := func(
		runContext context.Context,
	) error {
		switch calls.Add(1) {
		case 1:
			close(firstFailure)

			return errors.New(
				"RabbitMQ session unavailable",
			)

		default:
			close(secondSession)

			<-runContext.Done()

			return nil
		}
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerLifecycle(
			ctx,
			runner,
			10*time.Millisecond,
		)
	}()

	select {
	case <-firstFailure:
	case <-time.After(time.Second):
		t.Fatal(
			"first failed worker session never ran",
		)
	}

	select {
	case <-secondSession:
	case <-time.After(time.Second):
		t.Fatal(
			"worker did not retry after session failure",
		)
	}

	if calls.Load() < 2 {
		t.Fatalf(
			"expected at least two session attempts, got %d",
			calls.Load(),
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean lifecycle cancellation, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not stop after cancellation",
		)
	}
}

func TestRunWorkerLifecycleRetriesUnexpectedCleanSessionExit(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	secondSession := make(chan struct{})

	var calls atomic.Int32

	runner := func(
		runContext context.Context,
	) error {
		switch calls.Add(1) {
		case 1:
			// A session should normally return nil only because its context
			// was cancelled. Returning nil while the worker is still active
			// must not silently terminate the worker process.
			return nil

		default:
			close(secondSession)

			<-runContext.Done()

			return nil
		}
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerLifecycle(
			ctx,
			runner,
			10*time.Millisecond,
		)
	}()

	select {
	case <-secondSession:
	case <-time.After(time.Second):
		t.Fatal(
			"worker did not recover from unexpected clean session exit",
		)
	}

	if calls.Load() < 2 {
		t.Fatalf(
			"expected session restart after unexpected clean exit, got %d calls",
			calls.Load(),
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean lifecycle cancellation, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not stop after cancellation",
		)
	}
}

func TestRunWorkerLifecycleCancellationInterruptsRetryDelay(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	firstFailure := make(chan struct{})

	var calls atomic.Int32

	runner := func(
		context.Context,
	) error {
		calls.Add(1)

		select {
		case <-firstFailure:
		default:
			close(firstFailure)
		}

		return errors.New(
			"RabbitMQ temporarily unavailable",
		)
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerLifecycle(
			ctx,
			runner,
			time.Hour,
		)
	}()

	select {
	case <-firstFailure:
	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle never attempted first session",
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected cancellation during backoff to stop cleanly, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not interrupt retry delay on cancellation",
		)
	}

	if calls.Load() != 1 {
		t.Fatalf(
			"expected no retry after cancellation, got %d session attempts",
			calls.Load(),
		)
	}
}
