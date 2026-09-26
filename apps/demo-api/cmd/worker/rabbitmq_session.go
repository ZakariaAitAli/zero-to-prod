package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const rabbitMQWorkerSessionCloseTimeout = 2 * time.Second

type rabbitMQWorkerSessionChannel interface {
	rabbitMQConsumerChannel
}

type rabbitMQWorkerSessionConnection interface {
	OpenChannel() (
		rabbitMQWorkerSessionChannel,
		error,
	)
	CloseWithin(time.Duration) error
}

type rabbitMQWorkerDialer func(
	string,
) (
	rabbitMQWorkerSessionConnection,
	error,
)

type rabbitMQWorkerSession struct {
	connection rabbitMQWorkerSessionConnection
	deliveries <-chan amqp.Delivery
}

func newRabbitMQWorkerSessionWithDialer(
	ctx context.Context,
	connectionURL string,
	queueName string,
	dialer rabbitMQWorkerDialer,
) (*rabbitMQWorkerSession, error) {
	connection, err := dialer(connectionURL)
	if err != nil {
		return nil, fmt.Errorf(
			"connect to RabbitMQ worker broker: %w",
			err,
		)
	}

	channel, err := connection.OpenChannel()
	if err != nil {
		openErr := fmt.Errorf(
			"open RabbitMQ worker channel: %w",
			err,
		)

		closeErr := connection.CloseWithin(
			rabbitMQWorkerSessionCloseTimeout,
		)
		if closeErr != nil &&
			!errors.Is(closeErr, amqp.ErrClosed) {
			return nil, errors.Join(
				openErr,
				fmt.Errorf(
					"close RabbitMQ worker connection after channel-open failure: %w",
					closeErr,
				),
			)
		}

		return nil, openErr
	}

	deliveries, err := startRabbitMQConsumer(
		ctx,
		channel,
		queueName,
	)
	if err != nil {
		setupErr := fmt.Errorf(
			"configure RabbitMQ worker consumer: %w",
			err,
		)

		session := &rabbitMQWorkerSession{
			connection: connection,
		}

		if closeErr := session.Close(); closeErr != nil {
			return nil, errors.Join(
				setupErr,
				closeErr,
			)
		}

		return nil, setupErr
	}

	return &rabbitMQWorkerSession{
		connection: connection,
		deliveries: deliveries,
	}, nil
}

func (session *rabbitMQWorkerSession) Deliveries() <-chan amqp.Delivery {
	return session.deliveries
}

func (session *rabbitMQWorkerSession) Close() error {
	if session.connection == nil {
		return nil
	}

	if err := session.connection.CloseWithin(
		rabbitMQWorkerSessionCloseTimeout,
	); err != nil &&
		!errors.Is(err, amqp.ErrClosed) {
		return fmt.Errorf(
			"close RabbitMQ worker connection: %w",
			err,
		)
	}

	return nil
}
