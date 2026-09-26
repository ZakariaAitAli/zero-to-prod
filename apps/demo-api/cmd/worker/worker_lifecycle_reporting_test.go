package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWorkerLifecycleWithReporterReportsFailureBeforeRetry(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	sessionErr := errors.New(
		"RabbitMQ connection lost",
	)

	reported := make(
		chan error,
		1,
	)

	secondSession := make(
		chan struct{},
	)

	var calls atomic.Int32

	runner := func(
		runContext context.Context,
	) error {
		switch calls.Add(1) {
		case 1:
			return sessionErr

		default:
			select {
			case <-secondSession:
			default:
				close(secondSession)
			}

			<-runContext.Done()

			return nil
		}
	}

	reporter := func(err error) {
		reported <- err
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerLifecycleWithReporter(
			ctx,
			runner,
			10*time.Millisecond,
			reporter,
		)
	}()

	select {
	case err := <-reported:
		if !errors.Is(err, sessionErr) {
			t.Fatalf(
				"expected reported session error %v, got %v",
				sessionErr,
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not report failed session",
		)
	}

	select {
	case <-secondSession:
	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not retry after reporting failure",
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean cancellation, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not stop after cancellation",
		)
	}
}

func TestRunWorkerLifecycleWithReporterReportsUnexpectedCleanExit(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	reported := make(
		chan error,
		1,
	)

	secondSession := make(
		chan struct{},
	)

	var calls atomic.Int32

	runner := func(
		runContext context.Context,
	) error {
		switch calls.Add(1) {
		case 1:
			return nil

		default:
			select {
			case <-secondSession:
			default:
				close(secondSession)
			}

			<-runContext.Done()

			return nil
		}
	}

	reporter := func(err error) {
		reported <- err
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerLifecycleWithReporter(
			ctx,
			runner,
			10*time.Millisecond,
			reporter,
		)
	}()

	select {
	case err := <-reported:
		if err != nil {
			t.Fatalf(
				"expected nil to represent unexpected clean exit, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not report unexpected clean session exit",
		)
	}

	select {
	case <-secondSession:
	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not retry unexpected clean exit",
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean cancellation, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not stop after cancellation",
		)
	}
}

func TestRunWorkerLifecycleWithReporterDoesNotReportShutdownExit(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	sessionStarted := make(
		chan struct{},
	)

	var reports atomic.Int32

	runner := func(
		runContext context.Context,
	) error {
		close(sessionStarted)

		<-runContext.Done()

		return nil
	}

	reporter := func(error) {
		reports.Add(1)
	}

	result := make(
		chan error,
		1,
	)

	go func() {
		result <- runWorkerLifecycleWithReporter(
			ctx,
			runner,
			time.Hour,
			reporter,
		)
	}()

	select {
	case <-sessionStarted:
	case <-time.After(time.Second):
		t.Fatal(
			"worker session never started",
		)
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean shutdown, got %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal(
			"worker lifecycle did not stop after cancellation",
		)
	}

	if reports.Load() != 0 {
		t.Fatalf(
			"normal shutdown was reported as session exit %d times",
			reports.Load(),
		)
	}
}
