package main

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type workerDeliverySession interface {
	Deliveries() <-chan amqp.Delivery
	Close() error
}

type workerSessionFactory func(
	context.Context,
) (
	workerDeliverySession,
	error,
)

func runWorkerSession(
	ctx context.Context,
	store processingJobCompleter,
	newSession workerSessionFactory,
) error {
	session, err := newSession(ctx)
	if err != nil {
		return fmt.Errorf(
			"open worker delivery session: %w",
			err,
		)
	}

	loopErr := runWorkerDeliveryLoop(
		ctx,
		store,
		session.Deliveries(),
	)

	closeErr := session.Close()

	if loopErr != nil && closeErr != nil {
		return errors.Join(
			fmt.Errorf(
				"run worker delivery loop: %w",
				loopErr,
			),
			fmt.Errorf(
				"close worker delivery session: %w",
				closeErr,
			),
		)
	}

	if loopErr != nil {
		return fmt.Errorf(
			"run worker delivery loop: %w",
			loopErr,
		)
	}

	if closeErr != nil {
		return fmt.Errorf(
			"close worker delivery session: %w",
			closeErr,
		)
	}

	return nil
}
