package main

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRunWorkerDeliveryLoopStopsCleanlyOnContextCancellation(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	store := &stubProcessingJobCompleter{}

	var deliveries <-chan amqp.Delivery

	err := runWorkerDeliveryLoop(
		ctx,
		store,
		deliveries,
	)
	if err != nil {
		t.Fatalf(
			"expected clean cancellation, got %v",
			err,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"cancelled worker unexpectedly touched database %d times",
			store.calls,
		)
	}
}

func TestRunWorkerDeliveryLoopReportsUnexpectedDeliveryStreamClosure(
	t *testing.T,
) {
	deliveries := make(chan amqp.Delivery)
	close(deliveries)

	err := runWorkerDeliveryLoop(
		context.Background(),
		&stubProcessingJobCompleter{},
		deliveries,
	)

	if !errors.Is(
		err,
		errWorkerDeliveryStreamClosed,
	) {
		t.Fatalf(
			"expected errWorkerDeliveryStreamClosed, got %v",
			err,
		)
	}
}

func TestRunWorkerDeliveryLoopContinuesAfterPermanentReject(
	t *testing.T,
) {
	store := &stubProcessingJobCompleter{
		result: processingCompletion{
			Disposition: completionApplied,
		},
	}

	ack := &stubAcknowledger{}

	deliveries := make(
		chan amqp.Delivery,
		2,
	)

	deliveries <- amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  101,
		Body:         []byte(`{"type":`),
	}

	deliveries <- amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  102,
		Body: []byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":201,
			"work_item_id":301
		}`),
	}

	close(deliveries)

	err := runWorkerDeliveryLoop(
		context.Background(),
		store,
		deliveries,
	)

	if !errors.Is(
		err,
		errWorkerDeliveryStreamClosed,
	) {
		t.Fatalf(
			"expected stream-close result after processing buffered deliveries, got %v",
			err,
		)
	}

	if store.calls != 1 {
		t.Fatalf(
			"expected only valid message to touch database once, got %d",
			store.calls,
		)
	}

	if len(ack.calls) != 2 {
		t.Fatalf(
			"expected reject then ACK, got %d broker calls",
			len(ack.calls),
		)
	}

	if ack.calls[0].operation != "reject" {
		t.Fatalf(
			"expected first operation reject, got %q",
			ack.calls[0].operation,
		)
	}

	if ack.calls[0].requeue {
		t.Fatal(
			"permanent malformed-message reject unexpectedly requeued",
		)
	}

	if ack.calls[1].operation != "ack" {
		t.Fatalf(
			"expected second operation ACK, got %q",
			ack.calls[1].operation,
		)
	}
}

func TestRunWorkerDeliveryLoopLeavesSessionAfterTransientRequeue(
	t *testing.T,
) {
	storeErr := errors.New(
		"PostgreSQL temporarily unavailable",
	)

	store := &stubProcessingJobCompleter{
		err: storeErr,
	}

	ack := &stubAcknowledger{}

	deliveries := make(
		chan amqp.Delivery,
		1,
	)

	deliveries <- amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  103,
		Body: []byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":401,
			"work_item_id":501
		}`),
	}

	err := runWorkerDeliveryLoop(
		context.Background(),
		store,
		deliveries,
	)

	if !errors.Is(
		err,
		errWorkerDeliveryRequeued,
	) {
		t.Fatalf(
			"expected errWorkerDeliveryRequeued, got %v",
			err,
		)
	}

	if !errors.Is(err, storeErr) {
		t.Fatalf(
			"expected original transient store error %v, got %v",
			storeErr,
			err,
		)
	}

	if len(ack.calls) != 1 {
		t.Fatalf(
			"expected one NACK, got %d broker calls",
			len(ack.calls),
		)
	}

	call := ack.calls[0]

	if call.operation != "nack" {
		t.Fatalf(
			"expected NACK, got %q",
			call.operation,
		)
	}

	if call.multiple {
		t.Fatal(
			"transient failure unexpectedly NACKed multiple deliveries",
		)
	}

	if !call.requeue {
		t.Fatal(
			"transient failure did not request requeue",
		)
	}
}

func TestRunWorkerDeliveryLoopReturnsBrokerSettlementFailure(
	t *testing.T,
) {
	brokerErr := errors.New(
		"AMQP channel unavailable during ACK",
	)

	store := &stubProcessingJobCompleter{
		result: processingCompletion{
			Disposition: completionApplied,
		},
	}

	ack := &stubAcknowledger{
		err: brokerErr,
	}

	deliveries := make(
		chan amqp.Delivery,
		1,
	)

	deliveries <- amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  104,
		Body: []byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":601,
			"work_item_id":701
		}`),
	}

	err := runWorkerDeliveryLoop(
		context.Background(),
		store,
		deliveries,
	)

	if !errors.Is(err, brokerErr) {
		t.Fatalf(
			"expected broker settlement failure %v, got %v",
			brokerErr,
			err,
		)
	}

	if store.calls != 1 {
		t.Fatalf(
			"expected durable completion attempt before ACK failure, got %d calls",
			store.calls,
		)
	}

	if len(ack.calls) != 1 ||
		ack.calls[0].operation != "ack" {
		t.Fatalf(
			"expected one attempted ACK, got %+v",
			ack.calls,
		)
	}
}
