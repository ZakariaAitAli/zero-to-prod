package main

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var _ rabbitMQWorkerSessionChannel = (*amqp.Channel)(nil)

type amqpWorkerConnection struct {
	connection *amqp.Connection
}

var _ rabbitMQWorkerSessionConnection = (*amqpWorkerConnection)(nil)

func (connection *amqpWorkerConnection) OpenChannel() (
	rabbitMQWorkerSessionChannel,
	error,
) {
	channel, err := connection.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf(
			"open AMQP worker channel: %w",
			err,
		)
	}

	return channel, nil
}

func (connection *amqpWorkerConnection) CloseWithin(
	timeout time.Duration,
) error {
	return connection.connection.CloseDeadline(
		time.Now().Add(timeout),
	)
}

func dialRabbitMQWorker(
	connectionURL string,
) (
	rabbitMQWorkerSessionConnection,
	error,
) {
	connection, err := amqp.Dial(connectionURL)
	if err != nil {
		return nil, fmt.Errorf(
			"dial RabbitMQ worker: %w",
			err,
		)
	}

	return &amqpWorkerConnection{
		connection: connection,
	}, nil
}

func newRabbitMQWorkerSession(
	ctx context.Context,
	connectionURL string,
	queueName string,
) (*rabbitMQWorkerSession, error) {
	return newRabbitMQWorkerSessionWithDialer(
		ctx,
		connectionURL,
		queueName,
		dialRabbitMQWorker,
	)
}
