package main

import (
	"context"
	"errors"
	"fmt"
)

const outboxPublishFailureCode = "broker_publish_failed"

var (
	errNoOutboxMessage = errors.New("no unpublished outbox message")
	errBrokerPublish   = errors.New("broker publish failed")
)

type outboxMessage struct {
	ID              int64
	ProcessingJobID int64
	EventType       string
	Payload         []byte
	PublishAttempts int
}

type outboxPublisherStore interface {
	BeginOutboxPublishAttempt(context.Context) (outboxMessage, error)
	MarkOutboxPublished(context.Context, int64) error
	RecordOutboxPublishFailure(context.Context, int64, string) error
}

type brokerMessagePublisher interface {
	Publish(context.Context, outboxMessage) error
}

func publishNextOutboxMessage(
	ctx context.Context,
	store outboxPublisherStore,
	publisher brokerMessagePublisher,
) (bool, error) {
	message, err := store.BeginOutboxPublishAttempt(ctx)
	if errors.Is(err, errNoOutboxMessage) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"begin outbox publish attempt: %w",
			err,
		)
	}

	if err := publisher.Publish(ctx, message); err != nil {
		publishErr := fmt.Errorf(
			"publish outbox message: %w",
			errors.Join(errBrokerPublish, err),
		)

		if recordErr := store.RecordOutboxPublishFailure(
			ctx,
			message.ID,
			outboxPublishFailureCode,
		); recordErr != nil {
			return true, errors.Join(
				publishErr,
				fmt.Errorf(
					"record outbox publish failure: %w",
					recordErr,
				),
			)
		}

		return true, publishErr
	}

	if err := store.MarkOutboxPublished(
		ctx,
		message.ID,
	); err != nil {
		return true, fmt.Errorf(
			"mark outbox message published: %w",
			err,
		)
	}

	return true, nil
}
