package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type closeableBrokerMessagePublisher interface {
	brokerMessagePublisher
	Close() error
}

type brokerPublisherFactory func() (
	closeableBrokerMessagePublisher,
	error,
)

func waitForPublisherDelay(
	ctx context.Context,
	delay time.Duration,
) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false

	case <-timer.C:
		return true
	}
}

func runOutboxPublisher(
	ctx context.Context,
	store outboxPublisherStore,
	newPublisher brokerPublisherFactory,
	reconnectDelay time.Duration,
	idlePollDelay time.Duration,
	storeRetryDelay time.Duration,
) error {
	var publisher closeableBrokerMessagePublisher

	closePublisher := func() error {
		if publisher == nil {
			return nil
		}

		err := publisher.Close()
		publisher = nil

		if err != nil {
			return fmt.Errorf(
				"close outbox broker publisher: %w",
				err,
			)
		}

		return nil
	}

	for {
		if ctx.Err() != nil {
			return closePublisher()
		}

		if publisher == nil {
			var err error

			publisher, err = newPublisher()
			if err != nil {
				publisher = nil

				if !waitForPublisherDelay(
					ctx,
					reconnectDelay,
				) {
					return nil
				}

				continue
			}
		}

		processed, err := publishNextOutboxMessage(
			ctx,
			store,
			publisher,
		)
		if err != nil {
			if errors.Is(err, errBrokerPublish) {
				// The current AMQP connection/channel can no longer be
				// trusted. Close it and establish a fresh publisher before
				// attempting any more durable outbox work.
				_ = closePublisher()

				if !waitForPublisherDelay(
					ctx,
					reconnectDelay,
				) {
					return nil
				}

				continue
			}

			// Store-side failures do not imply that RabbitMQ is broken.
			// Keep the active publisher and retry the durable outbox later.
			if !waitForPublisherDelay(
				ctx,
				storeRetryDelay,
			) {
				return closePublisher()
			}

			continue
		}

		if processed {
			// Drain already-accepted work without an artificial sleep.
			continue
		}

		if !waitForPublisherDelay(
			ctx,
			idlePollDelay,
		) {
			return closePublisher()
		}
	}
}
