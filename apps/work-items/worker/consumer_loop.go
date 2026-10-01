package main

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	errWorkerDeliveryStreamClosed = errors.New(
		"worker delivery stream closed unexpectedly",
	)
	errWorkerDeliveryRequeued = errors.New(
		"worker delivery requeued after transient handling failure",
	)
)

func runWorkerDeliveryLoop(
	ctx context.Context,
	store processingJobCompleter,
	deliveries <-chan amqp.Delivery,
) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil

		case delivery, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}

				return errWorkerDeliveryStreamClosed
			}

			result, err := processRabbitMQDelivery(
				ctx,
				store,
				delivery,
			)
			if err != nil {
				return fmt.Errorf(
					"process worker RabbitMQ delivery: %w",
					err,
				)
			}

			if result.Settlement == settlementNackRequeue {
				if result.HandlingErr == nil {
					return errWorkerDeliveryRequeued
				}

				return fmt.Errorf(
					"leave worker session after requeue: %w",
					errors.Join(
						errWorkerDeliveryRequeued,
						result.HandlingErr,
					),
				)
			}
		}
	}
}
