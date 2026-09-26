package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunApplicationCancelsAndWaitsForBothServices(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(context.Background())

	httpStarted := make(chan struct{})
	httpStopped := make(chan struct{})

	runHTTP := func(ctx context.Context) error {
		close(httpStarted)

		<-ctx.Done()
		close(httpStopped)

		return nil
	}

	publisherStarted := make(chan struct{})
	publisherStopped := make(chan struct{})

	runPublisher := func(ctx context.Context) error {
		close(publisherStarted)

		<-ctx.Done()
		close(publisherStopped)

		return nil
	}

	result := make(chan error, 1)

	go func() {
		result <- runApplication(
			ctx,
			runHTTP,
			runPublisher,
		)
	}()

	for name, started := range map[string]<-chan struct{}{
		"HTTP":      httpStarted,
		"publisher": publisherStarted,
	} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("%s service did not start", name)
		}
	}

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf(
				"expected clean application shutdown, got: %v",
				err,
			)
		}

	case <-time.After(time.Second):
		t.Fatal("application did not stop after cancellation")
	}

	for name, stopped := range map[string]<-chan struct{}{
		"HTTP":      httpStopped,
		"publisher": publisherStopped,
	} {
		select {
		case <-stopped:
		default:
			t.Fatalf(
				"runApplication returned before %s stopped",
				name,
			)
		}
	}
}

func TestRunApplicationStopsHTTPWhenPublisherExitsUnexpectedly(
	t *testing.T,
) {
	httpStarted := make(chan struct{})
	httpStopped := make(chan struct{})

	runHTTP := func(ctx context.Context) error {
		close(httpStarted)

		<-ctx.Done()
		close(httpStopped)

		return nil
	}

	publisherErr := errors.New("publisher stopped unexpectedly")

	runPublisher := func(context.Context) error {
		return publisherErr
	}

	err := runApplication(
		context.Background(),
		runHTTP,
		runPublisher,
	)
	if err == nil {
		t.Fatal("expected publisher failure to fail application")
	}

	if !strings.Contains(err.Error(), "outbox publisher") {
		t.Fatalf(
			"expected publisher failure context, got: %v",
			err,
		)
	}

	if !errors.Is(err, publisherErr) {
		t.Fatalf(
			"expected original publisher error to be preserved, got: %v",
			err,
		)
	}

	select {
	case <-httpStarted:
	default:
		t.Fatal("HTTP service was never started")
	}

	select {
	case <-httpStopped:
	default:
		t.Fatal(
			"application returned before HTTP stopped after publisher failure",
		)
	}
}

func TestRunApplicationStopsPublisherWhenHTTPExitsUnexpectedly(
	t *testing.T,
) {
	publisherStarted := make(chan struct{})
	publisherStopped := make(chan struct{})

	runPublisher := func(ctx context.Context) error {
		close(publisherStarted)

		<-ctx.Done()
		close(publisherStopped)

		return nil
	}

	httpErr := errors.New("HTTP listener failed")

	runHTTP := func(context.Context) error {
		return httpErr
	}

	err := runApplication(
		context.Background(),
		runHTTP,
		runPublisher,
	)
	if err == nil {
		t.Fatal("expected HTTP failure to fail application")
	}

	if !strings.Contains(err.Error(), "HTTP server") {
		t.Fatalf(
			"expected HTTP failure context, got: %v",
			err,
		)
	}

	if !errors.Is(err, httpErr) {
		t.Fatalf(
			"expected original HTTP error to be preserved, got: %v",
			err,
		)
	}

	select {
	case <-publisherStarted:
	default:
		t.Fatal("publisher service was never started")
	}

	select {
	case <-publisherStopped:
	default:
		t.Fatal(
			"application returned before publisher stopped after HTTP failure",
		)
	}
}
