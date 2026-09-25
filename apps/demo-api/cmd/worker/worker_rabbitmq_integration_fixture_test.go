package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type workerRabbitMQIntegrationFixture struct {
	Store      *workerStore
	JobID      int64
	WorkItemID int64
	WorkerURL  string
	QueueName  string
}

func requireWorkerRabbitMQIntegrationFixture(
	t *testing.T,
	ctx context.Context,
) workerRabbitMQIntegrationFixture {
	t.Helper()

	workerURL := os.Getenv("RABBITMQ_WORKER_URL")
	fixtureURL := os.Getenv("RABBITMQ_FIXTURE_URL")

	if workerURL == "" ||
		fixtureURL == "" {
		t.Skip(
			"RABBITMQ_WORKER_URL and RABBITMQ_FIXTURE_URL are required",
		)
	}

	queueName := os.Getenv("RABBITMQ_QUEUE")
	if queueName == "" {
		queueName = defaultWorkerQueue
	}

	postgresFixture :=
		requireWorkerPostgresIntegrationFixture(
			t,
			ctx,
		)

	connection, err := amqp.Dial(fixtureURL)
	if err != nil {
		t.Fatalf(
			"connect RabbitMQ fixture identity: %v",
			err,
		)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()

		t.Fatalf(
			"open RabbitMQ fixture channel: %v",
			err,
		)
	}

	closeSetup := func() {
		if err := channel.Close(); err != nil &&
			err != amqp.ErrClosed {
			t.Errorf(
				"close RabbitMQ fixture channel: %v",
				err,
			)
		}

		if err := connection.Close(); err != nil &&
			err != amqp.ErrClosed {
			t.Errorf(
				"close RabbitMQ fixture connection: %v",
				err,
			)
		}
	}

	queue, err := channel.QueueInspect(queueName)
	if err != nil {
		closeSetup()

		t.Fatalf(
			"inspect RabbitMQ integration queue: %v",
			err,
		)
	}

	if queue.Messages != 0 ||
		queue.Consumers != 0 {
		closeSetup()

		t.Fatalf(
			"RabbitMQ integration queue is not isolated: messages=%d consumers=%d",
			queue.Messages,
			queue.Consumers,
		)
	}

	if err := channel.Confirm(false); err != nil {
		closeSetup()

		t.Fatalf(
			"enable RabbitMQ fixture publisher confirms: %v",
			err,
		)
	}

	payload := []byte(
		fmt.Sprintf(
			`{"type":"work_item.process","version":1,"job_id":%d,"work_item_id":%d}`,
			postgresFixture.JobID,
			postgresFixture.WorkItemID,
		),
	)

	confirmation, err :=
		channel.PublishWithDeferredConfirmWithContext(
			ctx,
			"",
			queueName,
			true,
			false,
			amqp.Publishing{
				ContentType:  "application/json",
				DeliveryMode: amqp.Persistent,
				Type:         "work_item.process",
				Body:         payload,
			},
		)
	if err != nil {
		closeSetup()

		t.Fatalf(
			"publish RabbitMQ integration fixture: %v",
			err,
		)
	}

	if confirmation == nil {
		closeSetup()

		t.Fatal(
			"RabbitMQ integration fixture publish returned no confirmation",
		)
	}

	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		closeSetup()

		t.Fatalf(
			"wait for RabbitMQ integration fixture confirmation: %v",
			err,
		)
	}

	if !acked {
		closeSetup()

		t.Fatal(
			"RabbitMQ negatively acknowledged integration fixture",
		)
	}

	var publishedQueue amqp.Queue

	for attempt := 0; attempt < 20; attempt++ {
		publishedQueue, err =
			channel.QueueInspect(queueName)
		if err != nil {
			closeSetup()

			t.Fatalf(
				"inspect published RabbitMQ integration fixture: %v",
				err,
			)
		}

		if publishedQueue.Messages == 1 {
			break
		}

		time.Sleep(25 * time.Millisecond)
	}

	if publishedQueue.Messages != 1 {
		closeSetup()

		t.Fatalf(
			"expected exactly one RabbitMQ fixture message, found %d",
			publishedQueue.Messages,
		)
	}

	closeSetup()

	t.Cleanup(func() {
		cleanupConnection, err := amqp.Dial(
			fixtureURL,
		)
		if err != nil {
			t.Errorf(
				"connect RabbitMQ fixture cleanup identity: %v",
				err,
			)

			return
		}

		defer cleanupConnection.Close()

		cleanupChannel, err :=
			cleanupConnection.Channel()
		if err != nil {
			t.Errorf(
				"open RabbitMQ fixture cleanup channel: %v",
				err,
			)

			return
		}

		defer cleanupChannel.Close()

		if _, err := cleanupChannel.QueuePurge(
			queueName,
			false,
		); err != nil {
			t.Errorf(
				"purge RabbitMQ integration fixture queue: %v",
				err,
			)
		}
	})

	return workerRabbitMQIntegrationFixture{
		Store:      postgresFixture.Store,
		JobID:      postgresFixture.JobID,
		WorkItemID: postgresFixture.WorkItemID,
		WorkerURL:  workerURL,
		QueueName:  queueName,
	}
}
