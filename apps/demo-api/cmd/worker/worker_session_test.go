package main

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type stubWorkerDeliverySession struct {
	deliveries <-chan amqp.Delivery

	closeCalls int
	closeErr   error
}

func (session *stubWorkerDeliverySession) Deliveries() <-chan amqp.Delivery {
	return session.deliveries
}

func (session *stubWorkerDeliverySession) Close() error {
	session.closeCalls++
	return session.closeErr
}

func TestRunWorkerSessionPropagatesFactoryFailure(
	t *testing.T,
) {
	factoryErr := errors.New(
		"RabbitMQ unavailable",
	)

	factoryCalls := 0

	factory := func(
		context.Context,
	) (
		workerDeliverySession,
		error,
	) {
		factoryCalls++

		return nil, factoryErr
	}

	err := runWorkerSession(
		context.Background(),
		&stubProcessingJobCompleter{},
		factory,
	)

	if !errors.Is(err, factoryErr) {
		t.Fatalf(
			"expected factory error %v, got %v",
			factoryErr,
			err,
		)
	}

	if factoryCalls != 1 {
		t.Fatalf(
			"expected one factory call, got %d",
			factoryCalls,
		)
	}
}

func TestRunWorkerSessionClosesSessionAfterCleanCancellation(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	session := &stubWorkerDeliverySession{}

	factory := func(
		factoryContext context.Context,
	) (
		workerDeliverySession,
		error,
	) {
		if factoryContext != ctx {
			t.Fatal(
				"worker session factory received different context",
			)
		}

		// Simulate shutdown arriving immediately after the broker session
		// was established but before another delivery is processed.
		cancel()

		return session, nil
	}

	err := runWorkerSession(
		ctx,
		&stubProcessingJobCompleter{},
		factory,
	)
	if err != nil {
		t.Fatalf(
			"expected clean cancellation, got %v",
			err,
		)
	}

	if session.closeCalls != 1 {
		t.Fatalf(
			"expected session cleanup once, got %d",
			session.closeCalls,
		)
	}
}

func TestRunWorkerSessionClosesSessionAfterDeliveryLoopFailure(
	t *testing.T,
) {
	deliveries := make(
		chan amqp.Delivery,
	)
	close(deliveries)

	session := &stubWorkerDeliverySession{
		deliveries: deliveries,
	}

	factory := func(
		context.Context,
	) (
		workerDeliverySession,
		error,
	) {
		return session, nil
	}

	err := runWorkerSession(
		context.Background(),
		&stubProcessingJobCompleter{},
		factory,
	)

	if !errors.Is(
		err,
		errWorkerDeliveryStreamClosed,
	) {
		t.Fatalf(
			"expected delivery-stream error, got %v",
			err,
		)
	}

	if session.closeCalls != 1 {
		t.Fatalf(
			"expected failed session cleanup once, got %d",
			session.closeCalls,
		)
	}
}

func TestRunWorkerSessionPreservesLoopAndCloseFailures(
	t *testing.T,
) {
	closeErr := errors.New(
		"RabbitMQ session cleanup failed",
	)

	deliveries := make(
		chan amqp.Delivery,
	)
	close(deliveries)

	session := &stubWorkerDeliverySession{
		deliveries: deliveries,
		closeErr:   closeErr,
	}

	factory := func(
		context.Context,
	) (
		workerDeliverySession,
		error,
	) {
		return session, nil
	}

	err := runWorkerSession(
		context.Background(),
		&stubProcessingJobCompleter{},
		factory,
	)

	if !errors.Is(
		err,
		errWorkerDeliveryStreamClosed,
	) {
		t.Fatalf(
			"expected delivery-stream failure, got %v",
			err,
		)
	}

	if !errors.Is(err, closeErr) {
		t.Fatalf(
			"expected cleanup failure %v, got %v",
			closeErr,
			err,
		)
	}

	if session.closeCalls != 1 {
		t.Fatalf(
			"expected one cleanup attempt, got %d",
			session.closeCalls,
		)
	}
}
