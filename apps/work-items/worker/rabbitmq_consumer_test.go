package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type consumeCall struct {
	queue     string
	consumer  string
	autoAck   bool
	exclusive bool
	noLocal   bool
	noWait    bool
	args      amqp.Table
}

type stubRabbitMQConsumerChannel struct {
	events *[]string

	qosPrefetchCount int
	qosPrefetchSize  int
	qosGlobal        bool
	qosCalls         int
	qosErr           error

	consumeContext context.Context
	consumeCall    consumeCall
	consumeCalls   int
	consumeResult  <-chan amqp.Delivery
	consumeErr     error
}

func (channel *stubRabbitMQConsumerChannel) Qos(
	prefetchCount int,
	prefetchSize int,
	global bool,
) error {
	channel.qosCalls++
	channel.qosPrefetchCount = prefetchCount
	channel.qosPrefetchSize = prefetchSize
	channel.qosGlobal = global

	if channel.events != nil {
		*channel.events = append(
			*channel.events,
			"qos",
		)
	}

	return channel.qosErr
}

func (channel *stubRabbitMQConsumerChannel) ConsumeWithContext(
	ctx context.Context,
	queue string,
	consumer string,
	autoAck bool,
	exclusive bool,
	noLocal bool,
	noWait bool,
	args amqp.Table,
) (<-chan amqp.Delivery, error) {
	channel.consumeCalls++
	channel.consumeContext = ctx
	channel.consumeCall = consumeCall{
		queue:     queue,
		consumer:  consumer,
		autoAck:   autoAck,
		exclusive: exclusive,
		noLocal:   noLocal,
		noWait:    noWait,
		args:      args,
	}

	if channel.events != nil {
		*channel.events = append(
			*channel.events,
			"consume",
		)
	}

	return channel.consumeResult, channel.consumeErr
}

func TestStartRabbitMQConsumerConfiguresPrefetchBeforeManualAckConsumption(
	t *testing.T,
) {
	events := make([]string, 0, 2)

	deliveries := make(
		chan amqp.Delivery,
	)

	ctx := context.WithValue(
		context.Background(),
		struct{}{},
		"consumer-context",
	)

	channel := &stubRabbitMQConsumerChannel{
		events:        &events,
		consumeResult: deliveries,
	}

	result, err := startRabbitMQConsumer(
		ctx,
		channel,
		"work_item_processing",
	)
	if err != nil {
		t.Fatalf(
			"start RabbitMQ consumer: %v",
			err,
		)
	}

	if result != deliveries {
		t.Fatal(
			"consumer did not return channel deliveries",
		)
	}

	expectedEvents := []string{
		"qos",
		"consume",
	}

	if !reflect.DeepEqual(
		events,
		expectedEvents,
	) {
		t.Fatalf(
			"expected setup order %v, got %v",
			expectedEvents,
			events,
		)
	}

	if channel.qosCalls != 1 {
		t.Fatalf(
			"expected one QoS call, got %d",
			channel.qosCalls,
		)
	}

	if channel.qosPrefetchCount != 1 {
		t.Fatalf(
			"expected prefetch count 1, got %d",
			channel.qosPrefetchCount,
		)
	}

	if channel.qosPrefetchSize != 0 {
		t.Fatalf(
			"expected prefetch size 0, got %d",
			channel.qosPrefetchSize,
		)
	}

	if channel.qosGlobal {
		t.Fatal(
			"expected channel-scoped QoS with global=false",
		)
	}

	if channel.consumeCalls != 1 {
		t.Fatalf(
			"expected one ConsumeWithContext call, got %d",
			channel.consumeCalls,
		)
	}

	if channel.consumeContext != ctx {
		t.Fatal(
			"ConsumeWithContext did not receive worker context",
		)
	}

	call := channel.consumeCall

	if call.queue != "work_item_processing" {
		t.Fatalf(
			"expected processing queue, got %q",
			call.queue,
		)
	}

	if call.consumer != workerConsumerTag {
		t.Fatalf(
			"expected consumer tag %q, got %q",
			workerConsumerTag,
			call.consumer,
		)
	}

	if call.autoAck {
		t.Fatal(
			"worker consumer unexpectedly enabled autoAck",
		)
	}

	if call.exclusive {
		t.Fatal(
			"worker consumer unexpectedly requested exclusive access",
		)
	}

	if call.noLocal {
		t.Fatal(
			"worker consumer unexpectedly enabled noLocal",
		)
	}

	if call.noWait {
		t.Fatal(
			"worker consumer unexpectedly enabled noWait",
		)
	}

	if call.args != nil {
		t.Fatalf(
			"expected nil consumer arguments, got %#v",
			call.args,
		)
	}
}

func TestStartRabbitMQConsumerStopsWhenQosFails(
	t *testing.T,
) {
	qosErr := errors.New(
		"RabbitMQ QoS unavailable",
	)

	channel := &stubRabbitMQConsumerChannel{
		qosErr: qosErr,
	}

	_, err := startRabbitMQConsumer(
		context.Background(),
		channel,
		"work_item_processing",
	)

	if !errors.Is(err, qosErr) {
		t.Fatalf(
			"expected QoS error %v, got %v",
			qosErr,
			err,
		)
	}

	if channel.qosCalls != 1 {
		t.Fatalf(
			"expected one QoS attempt, got %d",
			channel.qosCalls,
		)
	}

	if channel.consumeCalls != 0 {
		t.Fatalf(
			"consumer started despite QoS failure: %d calls",
			channel.consumeCalls,
		)
	}
}

func TestStartRabbitMQConsumerPropagatesConsumeFailure(
	t *testing.T,
) {
	consumeErr := errors.New(
		"RabbitMQ consume unavailable",
	)

	channel := &stubRabbitMQConsumerChannel{
		consumeErr: consumeErr,
	}

	_, err := startRabbitMQConsumer(
		context.Background(),
		channel,
		"work_item_processing",
	)

	if !errors.Is(err, consumeErr) {
		t.Fatalf(
			"expected consume error %v, got %v",
			consumeErr,
			err,
		)
	}

	if channel.qosCalls != 1 {
		t.Fatalf(
			"expected QoS before consume, got %d calls",
			channel.qosCalls,
		)
	}

	if channel.consumeCalls != 1 {
		t.Fatalf(
			"expected one consume attempt, got %d",
			channel.consumeCalls,
		)
	}
}
