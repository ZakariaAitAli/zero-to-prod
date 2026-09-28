package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type stubRabbitMQWorkerSessionChannel struct {
	*stubRabbitMQConsumerChannel

	events     *[]string
	closeCalls int
	closeErr   error
}

func (channel *stubRabbitMQWorkerSessionChannel) Close() error {
	channel.closeCalls++

	if channel.events != nil {
		*channel.events = append(
			*channel.events,
			"channel_close",
		)
	}

	return channel.closeErr
}

type stubRabbitMQWorkerSessionConnection struct {
	events *[]string

	channel rabbitMQWorkerSessionChannel

	openChannelCalls int
	openChannelErr   error

	closeCalls   int
	closeErr     error
	closeTimeout time.Duration
}

func (connection *stubRabbitMQWorkerSessionConnection) OpenChannel() (
	rabbitMQWorkerSessionChannel,
	error,
) {
	connection.openChannelCalls++

	if connection.events != nil {
		*connection.events = append(
			*connection.events,
			"channel_open",
		)
	}

	if connection.openChannelErr != nil {
		return nil, connection.openChannelErr
	}

	return connection.channel, nil
}

func (connection *stubRabbitMQWorkerSessionConnection) CloseWithin(
	timeout time.Duration,
) error {
	connection.closeCalls++
	connection.closeTimeout = timeout

	if connection.events != nil {
		*connection.events = append(
			*connection.events,
			"connection_close",
		)
	}

	return connection.closeErr
}

func TestNewRabbitMQWorkerSessionOwnsConfiguredConsumerResources(
	t *testing.T,
) {
	events := make([]string, 0, 6)

	deliveries := make(
		chan amqp.Delivery,
	)

	consumerChannel := &stubRabbitMQConsumerChannel{
		events:        &events,
		consumeResult: deliveries,
	}

	channel := &stubRabbitMQWorkerSessionChannel{
		stubRabbitMQConsumerChannel: consumerChannel,
		events:                      &events,
	}

	connection := &stubRabbitMQWorkerSessionConnection{
		events:  &events,
		channel: channel,
	}

	dialCalls := 0
	dialer := func(
		connectionURL string,
	) (
		rabbitMQWorkerSessionConnection,
		error,
	) {
		dialCalls++

		events = append(
			events,
			"dial",
		)

		if connectionURL != "amqp://worker.example/vhost" {
			t.Fatalf(
				"unexpected connection URL %q",
				connectionURL,
			)
		}

		return connection, nil
	}

	ctx := context.Background()

	session, err := newRabbitMQWorkerSessionWithDialer(
		ctx,
		"amqp://worker.example/vhost",
		"work_item_processing",
		dialer,
	)
	if err != nil {
		t.Fatalf(
			"open worker session: %v",
			err,
		)
	}

	if dialCalls != 1 {
		t.Fatalf(
			"expected one dial, got %d",
			dialCalls,
		)
	}

	if session.Deliveries() != deliveries {
		t.Fatal(
			"session did not expose configured deliveries",
		)
	}

	expectedSetup := []string{
		"dial",
		"channel_open",
		"qos",
		"consume",
	}

	if !reflect.DeepEqual(
		events,
		expectedSetup,
	) {
		t.Fatalf(
			"expected setup order %v, got %v",
			expectedSetup,
			events,
		)
	}

	if err := session.Close(); err != nil {
		t.Fatalf(
			"close worker session: %v",
			err,
		)
	}

	expectedAll := []string{
		"dial",
		"channel_open",
		"qos",
		"consume",
		"connection_close",
	}

	if !reflect.DeepEqual(
		events,
		expectedAll,
	) {
		t.Fatalf(
			"expected lifecycle order %v, got %v",
			expectedAll,
			events,
		)
	}

	if connection.closeTimeout !=
		rabbitMQWorkerSessionCloseTimeout {
		t.Fatalf(
			"expected bounded connection close timeout %s, got %s",
			rabbitMQWorkerSessionCloseTimeout,
			connection.closeTimeout,
		)
	}

	if channel.closeCalls != 0 {
		t.Fatalf(
			"session performed direct channel close %d times",
			channel.closeCalls,
		)
	}
}

func TestNewRabbitMQWorkerSessionReturnsDialFailureWithoutCleanup(
	t *testing.T,
) {
	dialErr := errors.New(
		"RabbitMQ unavailable",
	)

	dialer := func(
		string,
	) (
		rabbitMQWorkerSessionConnection,
		error,
	) {
		return nil, dialErr
	}

	session, err := newRabbitMQWorkerSessionWithDialer(
		context.Background(),
		"amqp://worker.example/vhost",
		"work_item_processing",
		dialer,
	)

	if session != nil {
		t.Fatal(
			"dial failure unexpectedly returned session",
		)
	}

	if !errors.Is(err, dialErr) {
		t.Fatalf(
			"expected dial error %v, got %v",
			dialErr,
			err,
		)
	}
}

func TestNewRabbitMQWorkerSessionClosesConnectionWhenChannelOpenFails(
	t *testing.T,
) {
	channelErr := errors.New(
		"RabbitMQ channel unavailable",
	)

	connection := &stubRabbitMQWorkerSessionConnection{
		openChannelErr: channelErr,
	}

	dialer := func(
		string,
	) (
		rabbitMQWorkerSessionConnection,
		error,
	) {
		return connection, nil
	}

	session, err := newRabbitMQWorkerSessionWithDialer(
		context.Background(),
		"amqp://worker.example/vhost",
		"work_item_processing",
		dialer,
	)

	if session != nil {
		t.Fatal(
			"channel failure unexpectedly returned session",
		)
	}

	if !errors.Is(err, channelErr) {
		t.Fatalf(
			"expected channel error %v, got %v",
			channelErr,
			err,
		)
	}

	if connection.closeCalls != 1 {
		t.Fatalf(
			"expected connection cleanup once, got %d",
			connection.closeCalls,
		)
	}
}

func TestNewRabbitMQWorkerSessionClosesResourcesWhenConsumerSetupFails(
	t *testing.T,
) {
	setupErr := errors.New(
		"RabbitMQ QoS unavailable",
	)

	consumerChannel := &stubRabbitMQConsumerChannel{
		qosErr: setupErr,
	}

	channel := &stubRabbitMQWorkerSessionChannel{
		stubRabbitMQConsumerChannel: consumerChannel,
	}

	connection := &stubRabbitMQWorkerSessionConnection{
		channel: channel,
	}

	dialer := func(
		string,
	) (
		rabbitMQWorkerSessionConnection,
		error,
	) {
		return connection, nil
	}

	session, err := newRabbitMQWorkerSessionWithDialer(
		context.Background(),
		"amqp://worker.example/vhost",
		"work_item_processing",
		dialer,
	)

	if session != nil {
		t.Fatal(
			"consumer setup failure unexpectedly returned session",
		)
	}

	if !errors.Is(err, setupErr) {
		t.Fatalf(
			"expected consumer setup error %v, got %v",
			setupErr,
			err,
		)
	}

	if channel.closeCalls != 0 {
		t.Fatalf(
			"consumer setup failure performed direct channel close %d times",
			channel.closeCalls,
		)
	}

	if connection.closeCalls != 1 {
		t.Fatalf(
			"expected connection cleanup once, got %d",
			connection.closeCalls,
		)
	}
}

func TestRabbitMQWorkerSessionCloseUsesBoundedConnectionShutdown(
	t *testing.T,
) {
	connectionErr := errors.New(
		"close connection failed",
	)

	events := make([]string, 0, 1)

	channel := &stubRabbitMQWorkerSessionChannel{
		stubRabbitMQConsumerChannel: &stubRabbitMQConsumerChannel{},
		events:                      &events,
	}

	connection := &stubRabbitMQWorkerSessionConnection{
		events:   &events,
		closeErr: connectionErr,
	}

	session := &rabbitMQWorkerSession{
		connection: connection,
	}

	err := session.Close()

	if !errors.Is(err, connectionErr) {
		t.Fatalf(
			"expected connection close error %v, got %v",
			connectionErr,
			err,
		)
	}

	if connection.closeCalls != 1 {
		t.Fatalf(
			"expected one bounded connection close, got %d",
			connection.closeCalls,
		)
	}

	if connection.closeTimeout !=
		rabbitMQWorkerSessionCloseTimeout {
		t.Fatalf(
			"expected close timeout %s, got %s",
			rabbitMQWorkerSessionCloseTimeout,
			connection.closeTimeout,
		)
	}

	if channel.closeCalls != 0 {
		t.Fatalf(
			"session performed direct channel close %d times",
			channel.closeCalls,
		)
	}

	expectedEvents := []string{
		"connection_close",
	}

	if !reflect.DeepEqual(
		events,
		expectedEvents,
	) {
		t.Fatalf(
			"expected bounded connection cleanup %v, got %v",
			expectedEvents,
			events,
		)
	}
}
