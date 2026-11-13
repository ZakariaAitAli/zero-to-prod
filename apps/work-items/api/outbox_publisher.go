package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	CreatedAt       time.Time
	// TraceParent is the W3C traceparent stored at acceptance, or "".
	TraceParent string
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
		outboxPublishResults.WithLabelValues("store_error").Inc()
		return false, fmt.Errorf(
			"begin outbox publish attempt: %w",
			err,
		)
	}

	// The publish continues the trace of the request that accepted the job,
	// even though that request finished earlier.
	ctx, span := apiTracer.Start(
		observability.ContextWithTraceParent(ctx, message.TraceParent),
		message.EventType+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation.type", "send"),
			attribute.Int64("work_items.outbox_id", message.ID),
			attribute.Int64("work_items.job_id", message.ProcessingJobID),
			attribute.Int("work_items.publish_attempt", message.PublishAttempts),
		),
	)
	defer span.End()

	started := time.Now()
	publishErr := publisher.Publish(ctx, message)
	outboxPublishDuration.Observe(time.Since(started).Seconds())

	if publishErr != nil {
		outboxPublishResults.WithLabelValues("broker_error").Inc()
		span.SetStatus(codes.Error, "broker publish failed")

		publishErr = fmt.Errorf(
			"publish outbox message: %w",
			errors.Join(errBrokerPublish, publishErr),
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
		// The broker holds the message; the row stays unpublished and is sent
		// again, a duplicate the worker already tolerates.
		outboxPublishResults.WithLabelValues("store_error").Inc()
		span.SetStatus(codes.Error, "mark published failed")
		return true, fmt.Errorf(
			"mark outbox message published: %w",
			err,
		)
	}

	outboxPublishResults.WithLabelValues("published").Inc()
	if !message.CreatedAt.IsZero() {
		outboxPublishLag.Observe(time.Since(message.CreatedAt).Seconds())
	}

	return true, nil
}
