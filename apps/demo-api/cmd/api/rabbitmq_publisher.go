package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	errRabbitMQPublishNacked    = errors.New("RabbitMQ negatively acknowledged publication")
	errRabbitMQPublishReturned  = errors.New("RabbitMQ returned unroutable publication")
	errRabbitMQConfirmMissing   = errors.New("RabbitMQ publisher confirmation unavailable")
	errRabbitMQReturnChanClosed = errors.New("RabbitMQ return notification channel closed")
)

type rabbitMQPublisher struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	returns    <-chan amqp.Return
	queueName  string
}

func newRabbitMQPublisher(
	connectionURL string,
	queueName string,
) (*rabbitMQPublisher, error) {
	connection, err := amqp.Dial(connectionURL)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()

		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}

	returns := channel.NotifyReturn(
		make(chan amqp.Return, 1),
	)

	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		return nil, fmt.Errorf(
			"enable RabbitMQ publisher confirms: %w",
			err,
		)
	}

	return &rabbitMQPublisher{
		connection: connection,
		channel:    channel,
		returns:    returns,
		queueName:  queueName,
	}, nil
}

func (publisher *rabbitMQPublisher) Publish(
	ctx context.Context,
	message outboxMessage,
) error {
	confirmation, err := publisher.channel.
		PublishWithDeferredConfirmWithContext(
			ctx,
			"",
			publisher.queueName,
			true,
			false,
			amqp.Publishing{
				ContentType:   "application/json",
				DeliveryMode:  amqp.Persistent,
				MessageId:     strconv.FormatInt(message.ID, 10),
				CorrelationId: strconv.FormatInt(message.ProcessingJobID, 10),
				Type:          message.EventType,
				Timestamp:     time.Now().UTC(),
				Body:          message.Payload,
			},
		)
	if err != nil {
		return fmt.Errorf(
			"publish RabbitMQ message: %w",
			err,
		)
	}

	if confirmation == nil {
		return errRabbitMQConfirmMissing
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf(
			"wait for RabbitMQ publisher confirm: %w",
			err,
		)
	}

	// RabbitMQ sends basic.return for an unroutable mandatory publication
	// before sending its publisher confirmation. Because this publisher
	// permits only one in-flight message per call, the buffered return
	// channel can be checked after the confirmation without ambiguity.
	select {
	case returned, ok := <-publisher.returns:
		if !ok {
			return errRabbitMQReturnChanClosed
		}

		return fmt.Errorf(
			"%w: reply_code=%d reply_text=%q exchange=%q routing_key=%q",
			errRabbitMQPublishReturned,
			returned.ReplyCode,
			returned.ReplyText,
			returned.Exchange,
			returned.RoutingKey,
		)

	default:
	}

	if !acked {
		return errRabbitMQPublishNacked
	}

	return nil
}

func (publisher *rabbitMQPublisher) Close() error {
	var closeErrors []error

	if publisher.channel != nil {
		if err := publisher.channel.Close(); err != nil &&
			!errors.Is(err, amqp.ErrClosed) {
			closeErrors = append(
				closeErrors,
				fmt.Errorf("close RabbitMQ channel: %w", err),
			)
		}
	}

	if publisher.connection != nil {
		if err := publisher.connection.Close(); err != nil &&
			!errors.Is(err, amqp.ErrClosed) {
			closeErrors = append(
				closeErrors,
				fmt.Errorf("close RabbitMQ connection: %w", err),
			)
		}
	}

	return errors.Join(closeErrors...)
}
