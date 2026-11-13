package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

type publisherState string

const (
	publisherHealthy           publisherState = "healthy"
	publisherBrokerUnavailable publisherState = "broker_unavailable"
	publisherStoreUnavailable  publisherState = "store_unavailable"
)

// publisherCondition logs the outbox publisher's state when it changes. The
// zero value logs nothing until the first failure, so a healthy start is quiet.
type publisherCondition struct {
	state publisherState
}

func (c *publisherCondition) set(ctx context.Context, state publisherState, err error) {
	if c.state == state || (c.state == "" && state == publisherHealthy) {
		c.state = state
		return
	}
	c.state = state

	if state == publisherHealthy {
		slog.InfoContext(ctx, "outbox publisher recovered", "state", state)
		return
	}
	slog.WarnContext(ctx, "outbox publisher degraded", "state", state, "error", err)
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

	// Failures repeat every retry delay while a dependency is down, so only
	// state changes are logged; the counters carry the rate.
	var condition publisherCondition

	for {
		if ctx.Err() != nil {
			return closePublisher()
		}

		if publisher == nil {
			var err error

			publisher, err = newPublisher()
			if err != nil {
				publisher = nil
				outboxBrokerConnectFailures.Inc()
				condition.set(ctx, publisherBrokerUnavailable, err)

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
				condition.set(ctx, publisherBrokerUnavailable, err)

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

			condition.set(ctx, publisherStoreUnavailable, err)

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

		condition.set(ctx, publisherHealthy, nil)

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
