package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/observability"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	return processRabbitMQDeliveryWithProcessor(
		ctx,
		store,
		processingJobProcessorFunc(
			successfulProcessingJobProcessor,
		),
		delivery,
	)
}

func processRabbitMQDeliveryWithProcessor(
	ctx context.Context,
	store processingJobCompleter,
	processor processingJobProcessor,
	delivery amqp.Delivery,
) (deliveryProcessingResult, error) {
	started := time.Now()

	// Each delivery attempt is one consumer span in the trace the API started
	// at acceptance. Only the API's publisher user may write to the queue, so
	// the traceparent header comes from our own publisher.
	ctx, span := workerTracer.Start(
		observability.ExtractHeaders(ctx, observability.HeaderCarrier(delivery.Headers)),
		workItemProcessMessageType+" process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.message.id", delivery.MessageId),
			attribute.Bool("work_items.redelivered", delivery.Redelivered),
		),
	)
	defer span.End()

	// Decoded here only to correlate logs and spans; handling validates the
	// message itself and settles malformed ones.
	logAttrs := []any{"redelivered", delivery.Redelivered}
	if message, err := decodeWorkerMessage(delivery.Body); err == nil {
		span.SetAttributes(
			attribute.Int64("work_items.job_id", message.JobID),
			attribute.Int64("work_items.work_item_id", message.WorkItemID),
			attribute.String("work_items.delivery_id", message.DeliveryID),
		)
		logAttrs = append(logAttrs,
			"job_id", message.JobID,
			"work_item_id", message.WorkItemID,
			"delivery_id", message.DeliveryID,
		)
	}

	settlement, handlingErr := handleWorkerMessageWithProcessor(
		ctx,
		store,
		processor,
		delivery.Body,
	)

	result := deliveryProcessingResult{
		Settlement:  settlement,
		HandlingErr: handlingErr,
	}

	settleErr := settleRabbitMQDelivery(
		delivery,
		settlement,
	)

	outcome := string(settlement)
	if settleErr != nil {
		outcome = "settle_failed"
	}
	elapsed := time.Since(started)
	workerDeliveries.WithLabelValues(outcome, strconv.FormatBool(delivery.Redelivered)).Inc()
	workerDeliveryDuration.WithLabelValues(outcome).Observe(elapsed.Seconds())

	span.SetAttributes(attribute.String("work_items.settlement", outcome))
	logAttrs = append(logAttrs,
		"settlement", outcome,
		"duration_ms", float64(elapsed.Microseconds())/1000,
	)

	switch {
	case settleErr != nil:
		span.SetStatus(codes.Error, outcome)
		slog.ErrorContext(ctx, "delivery settlement failed", append(logAttrs, "error", settleErr)...)
	case handlingErr != nil:
		span.SetStatus(codes.Error, outcome)
		slog.WarnContext(ctx, "delivery handled with error", append(logAttrs, "error", handlingErr)...)
	default:
		slog.InfoContext(ctx, "delivery settled", logAttrs...)
	}

	if settleErr != nil {
		return result, fmt.Errorf(
			"settle processed RabbitMQ delivery: %w",
			settleErr,
		)
	}

	return result, nil
}
