package main

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

func settleRabbitMQDelivery(
	delivery amqp.Delivery,
	settlement deliverySettlement,
) error {
	switch settlement {
	case settlementAck:
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf(
				"ack RabbitMQ delivery: %w",
				err,
			)
		}

		return nil

	case settlementReject:
		if err := delivery.Reject(false); err != nil {
			return fmt.Errorf(
				"reject RabbitMQ delivery: %w",
				err,
			)
		}

		return nil

	case settlementNackRequeue:
		if err := delivery.Nack(false, true); err != nil {
			return fmt.Errorf(
				"nack RabbitMQ delivery for requeue: %w",
				err,
			)
		}

		return nil

	default:
		return fmt.Errorf(
			"unknown delivery settlement %q",
			settlement,
		)
	}
}
