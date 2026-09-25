package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type orderedCompleter struct {
	events *[]string
	result processingCompletion
	err    error
}

func (store *orderedCompleter) CompleteProcessingJob(
	_ context.Context,
	_ int64,
	_ int64,
) (processingCompletion, error) {
	*store.events = append(
		*store.events,
		"database_completion",
	)

	return store.result, store.err
}

type orderedAcknowledger struct {
	events *[]string
	err    error
}

func (ack *orderedAcknowledger) Ack(
	_ uint64,
	_ bool,
) error {
	*ack.events = append(
		*ack.events,
		"ack",
	)

	return ack.err
}

func (ack *orderedAcknowledger) Reject(
	_ uint64,
	_ bool,
) error {
	*ack.events = append(
		*ack.events,
		"reject",
	)

	return ack.err
}

func (ack *orderedAcknowledger) Nack(
	_ uint64,
	_ bool,
	_ bool,
) error {
	*ack.events = append(
		*ack.events,
		"nack",
	)

	return ack.err
}

func validWorkerDelivery(
	ack amqp.Acknowledger,
) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  42,
		Body: []byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":101,
			"work_item_id":201
		}`),
	}
}

func TestProcessRabbitMQDeliveryCompletesDatabaseBeforeAck(
	t *testing.T,
) {
	events := make([]string, 0, 2)

	store := &orderedCompleter{
		events: &events,
		result: processingCompletion{
			Disposition: completionApplied,
		},
	}

	ack := &orderedAcknowledger{
		events: &events,
	}

	result, err := processRabbitMQDelivery(
		context.Background(),
		store,
		validWorkerDelivery(ack),
	)
	if err != nil {
		t.Fatalf("process delivery: %v", err)
	}

	if result.Settlement != settlementAck {
		t.Fatalf(
			"expected ACK settlement, got %q",
			result.Settlement,
		)
	}

	if result.HandlingErr != nil {
		t.Fatalf(
			"unexpected handling error: %v",
			result.HandlingErr,
		)
	}

	expectedEvents := []string{
		"database_completion",
		"ack",
	}

	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf(
			"expected ordering %v, got %v",
			expectedEvents,
			events,
		)
	}
}

func TestProcessRabbitMQDeliveryRejectsMalformedMessageWithoutDatabaseCall(
	t *testing.T,
) {
	events := make([]string, 0, 1)

	store := &orderedCompleter{
		events: &events,
	}

	ack := &orderedAcknowledger{
		events: &events,
	}

	delivery := amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  43,
		Body:         []byte(`{"type":`),
	}

	result, err := processRabbitMQDelivery(
		context.Background(),
		store,
		delivery,
	)
	if err != nil {
		t.Fatalf(
			"successful permanent rejection returned infrastructure error: %v",
			err,
		)
	}

	if result.Settlement != settlementReject {
		t.Fatalf(
			"expected reject settlement, got %q",
			result.Settlement,
		)
	}

	if result.HandlingErr == nil {
		t.Fatal("expected malformed-message handling error")
	}

	expectedEvents := []string{
		"reject",
	}

	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf(
			"expected no database call and one reject %v, got %v",
			expectedEvents,
			events,
		)
	}
}

func TestProcessRabbitMQDeliveryNacksTransientStoreFailureAfterDatabaseAttempt(
	t *testing.T,
) {
	events := make([]string, 0, 2)

	storeErr := errors.New(
		"PostgreSQL temporarily unavailable",
	)

	store := &orderedCompleter{
		events: &events,
		err:    storeErr,
	}

	ack := &orderedAcknowledger{
		events: &events,
	}

	result, err := processRabbitMQDelivery(
		context.Background(),
		store,
		validWorkerDelivery(ack),
	)
	if err != nil {
		t.Fatalf(
			"successful requeue returned infrastructure error: %v",
			err,
		)
	}

	if result.Settlement != settlementNackRequeue {
		t.Fatalf(
			"expected NACK/requeue settlement, got %q",
			result.Settlement,
		)
	}

	if !errors.Is(result.HandlingErr, storeErr) {
		t.Fatalf(
			"expected handling error %v, got %v",
			storeErr,
			result.HandlingErr,
		)
	}

	expectedEvents := []string{
		"database_completion",
		"nack",
	}

	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf(
			"expected ordering %v, got %v",
			expectedEvents,
			events,
		)
	}
}

func TestProcessRabbitMQDeliveryReturnsSettlementFailureAfterCompletion(
	t *testing.T,
) {
	events := make([]string, 0, 2)

	brokerErr := errors.New(
		"AMQP channel unavailable during ACK",
	)

	store := &orderedCompleter{
		events: &events,
		result: processingCompletion{
			Disposition: completionApplied,
		},
	}

	ack := &orderedAcknowledger{
		events: &events,
		err:    brokerErr,
	}

	result, err := processRabbitMQDelivery(
		context.Background(),
		store,
		validWorkerDelivery(ack),
	)

	if !errors.Is(err, brokerErr) {
		t.Fatalf(
			"expected settlement failure %v, got %v",
			brokerErr,
			err,
		)
	}

	if result.Settlement != settlementAck {
		t.Fatalf(
			"expected attempted ACK settlement, got %q",
			result.Settlement,
		)
	}

	if result.HandlingErr != nil {
		t.Fatalf(
			"unexpected handling error: %v",
			result.HandlingErr,
		)
	}

	expectedEvents := []string{
		"database_completion",
		"ack",
	}

	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf(
			"database completion must precede failed ACK: expected %v, got %v",
			expectedEvents,
			events,
		)
	}
}

func TestProcessRabbitMQDeliveryStillAttemptsRejectWhenHandlingReturnsError(
	t *testing.T,
) {
	events := make([]string, 0, 1)

	brokerErr := errors.New(
		"AMQP channel unavailable during reject",
	)

	store := &orderedCompleter{
		events: &events,
	}

	ack := &orderedAcknowledger{
		events: &events,
		err:    brokerErr,
	}

	delivery := amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  44,
		Body:         []byte(`{"malformed":`),
	}

	result, err := processRabbitMQDelivery(
		context.Background(),
		store,
		delivery,
	)

	if result.Settlement != settlementReject {
		t.Fatalf(
			"expected reject settlement, got %q",
			result.Settlement,
		)
	}

	if result.HandlingErr == nil {
		t.Fatal("expected malformed-message handling error")
	}

	if !errors.Is(err, brokerErr) {
		t.Fatalf(
			"expected reject settlement failure %v, got %v",
			brokerErr,
			err,
		)
	}

	expectedEvents := []string{
		"reject",
	}

	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf(
			"expected reject attempt despite handling error %v, got %v",
			expectedEvents,
			events,
		)
	}
}

type orderedProcessingJobProcessor struct {
	events *[]string
	err    error
}

func (processor *orderedProcessingJobProcessor) Process(
	_ context.Context,
	_ workerMessage,
) error {
	*processor.events = append(
		*processor.events,
		"processing_attempt",
	)

	return processor.err
}

func TestProcessRabbitMQDeliveryRequeuesProcessingFailureBeforeDatabaseCompletion(
	t *testing.T,
) {
	events := make([]string, 0, 2)

	processingErr := errors.New(
		"representative processing temporarily failed",
	)

	processor := &orderedProcessingJobProcessor{
		events: &events,
		err:    processingErr,
	}

	store := &orderedCompleter{
		events: &events,
	}

	ack := &orderedAcknowledger{
		events: &events,
	}

	result, err := processRabbitMQDeliveryWithProcessor(
		context.Background(),
		store,
		processor,
		validWorkerDelivery(ack),
	)
	if err != nil {
		t.Fatalf(
			"successful processing-failure requeue returned broker error: %v",
			err,
		)
	}

	if result.Settlement != settlementNackRequeue {
		t.Fatalf(
			"expected NACK/requeue settlement, got %q",
			result.Settlement,
		)
	}

	if !errors.Is(
		result.HandlingErr,
		processingErr,
	) {
		t.Fatalf(
			"expected processing error %v, got %v",
			processingErr,
			result.HandlingErr,
		)
	}

	expectedEvents := []string{
		"processing_attempt",
		"nack",
	}

	if !reflect.DeepEqual(
		events,
		expectedEvents,
	) {
		t.Fatalf(
			"expected processing failure ordering %v, got %v",
			expectedEvents,
			events,
		)
	}
}
