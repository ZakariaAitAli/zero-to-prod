package main

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

const workerConsumerTag = "zero-to-prod-worker"

type rabbitMQConsumerChannel interface {
	Qos(
		prefetchCount int,
		prefetchSize int,
		global bool,
	) error

	ConsumeWithContext(
		context.Context,
		string,
		string,
		bool,
		bool,
		bool,
		bool,
		amqp.Table,
	) (<-chan amqp.Delivery, error)
}

func startRabbitMQConsumer(
	ctx context.Context,
	channel rabbitMQConsumerChannel,
	queueName string,
) (<-chan amqp.Delivery, error) {
	if err := channel.Qos(
		1,
		0,
		false,
	); err != nil {
		return nil, fmt.Errorf(
			"configure RabbitMQ worker QoS: %w",
			err,
		)
	}

	deliveries, err := channel.ConsumeWithContext(
		ctx,
		queueName,
		workerConsumerTag,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"start RabbitMQ worker consumer: %w",
			err,
		)
	}

	return deliveries, nil
}
