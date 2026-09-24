package main

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type deliveryProcessingResult struct {
	Settlement  deliverySettlement
	HandlingErr error
}

func processRabbitMQDelivery(
	ctx context.Context,
	store processingJobCompleter,
	delivery amqp.Delivery,
) (deliveryProcessingResult, error) {
	settlement, handlingErr := handleWorkerMessage(
		ctx,
		store,
		delivery.Body,
	)

	result := deliveryProcessingResult{
		Settlement:  settlement,
		HandlingErr: handlingErr,
	}

	if err := settleRabbitMQDelivery(
		delivery,
		settlement,
	); err != nil {
		return result, fmt.Errorf(
			"settle processed RabbitMQ delivery: %w",
			err,
		)
	}

	return result, nil
}
