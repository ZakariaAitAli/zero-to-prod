package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubOutboxPublisherStore struct {
	beginMessage outboxMessage
	beginErr     error
	beginCalls   int

	markedPublishedID int64
	markErr           error

	failureMessageID int64
	failureCode      string
	failureErr       error
}

func (store *stubOutboxPublisherStore) BeginOutboxPublishAttempt(
	context.Context,
) (outboxMessage, error) {
	store.beginCalls++
	return store.beginMessage, store.beginErr
}

func (store *stubOutboxPublisherStore) MarkOutboxPublished(
	_ context.Context,
	messageID int64,
) error {
	store.markedPublishedID = messageID
	return store.markErr
}

func (store *stubOutboxPublisherStore) RecordOutboxPublishFailure(
	_ context.Context,
	messageID int64,
	code string,
) error {
	store.failureMessageID = messageID
	store.failureCode = code

	return store.failureErr
}

type stubBrokerMessagePublisher struct {
	publishedMessage outboxMessage
	publishCalls     int
	publishErr       error
}

func (publisher *stubBrokerMessagePublisher) Publish(
	_ context.Context,
	message outboxMessage,
) error {
	publisher.publishCalls++
	publisher.publishedMessage = message

	return publisher.publishErr
}

func TestPublishNextOutboxMessagePublishesAndMarksSuccess(t *testing.T) {
	message := outboxMessage{
		ID:              11,
		ProcessingJobID: 7,
		EventType:       "work_item.process",
		Payload: []byte(
			`{"type":"work_item.process","version":1,"job_id":7,"work_item_id":42}`,
		),
		PublishAttempts: 1,
	}

	store := &stubOutboxPublisherStore{
		beginMessage: message,
	}

	broker := &stubBrokerMessagePublisher{}

	processed, err := publishNextOutboxMessage(
		context.Background(),
		store,
		broker,
	)
	if err != nil {
		t.Fatalf("expected publishing to succeed, got: %v", err)
	}

	if !processed {
		t.Fatal("expected one outbox message to be processed")
	}

	if store.beginCalls != 1 {
		t.Fatalf(
			"expected one begin-attempt call, got %d",
			store.beginCalls,
		)
	}

	if broker.publishCalls != 1 {
		t.Fatalf(
			"expected one broker publish, got %d",
			broker.publishCalls,
		)
	}

	if broker.publishedMessage.ID != message.ID {
		t.Fatalf(
			"expected outbox message %d, got %d",
			message.ID,
			broker.publishedMessage.ID,
		)
	}

	if store.markedPublishedID != message.ID {
		t.Fatalf(
			"expected outbox message %d to be marked published, got %d",
			message.ID,
			store.markedPublishedID,
		)
	}

	if store.failureMessageID != 0 {
		t.Fatalf(
			"successful publish unexpectedly recorded failure for message %d",
			store.failureMessageID,
		)
	}
}

func TestPublishNextOutboxMessageDoesNothingWhenOutboxIsEmpty(t *testing.T) {
	store := &stubOutboxPublisherStore{
		beginErr: errNoOutboxMessage,
	}

	broker := &stubBrokerMessagePublisher{}

	processed, err := publishNextOutboxMessage(
		context.Background(),
		store,
		broker,
	)
	if err != nil {
		t.Fatalf("expected empty outbox not to fail, got: %v", err)
	}

	if processed {
		t.Fatal("expected no outbox message to be processed")
	}

	if store.beginCalls != 1 {
		t.Fatalf(
			"expected one begin-attempt call, got %d",
			store.beginCalls,
		)
	}

	if broker.publishCalls != 0 {
		t.Fatalf(
			"broker was called for an empty outbox: %d",
			broker.publishCalls,
		)
	}

	if store.markedPublishedID != 0 {
		t.Fatalf(
			"empty outbox unexpectedly marked message %d published",
			store.markedPublishedID,
		)
	}
}

func TestPublishNextOutboxMessageRecordsBrokerFailure(t *testing.T) {
	message := outboxMessage{
		ID:              21,
		ProcessingJobID: 9,
		EventType:       "work_item.process",
		Payload:         []byte(`{"job_id":9}`),
		PublishAttempts: 3,
	}

	store := &stubOutboxPublisherStore{
		beginMessage: message,
	}

	broker := &stubBrokerMessagePublisher{
		publishErr: errors.New("broker unavailable"),
	}

	processed, err := publishNextOutboxMessage(
		context.Background(),
		store,
		broker,
	)
	if err == nil {
		t.Fatal("expected broker publishing failure")
	}

	if !processed {
		t.Fatal("expected the selected outbox message to count as processed")
	}

	if !strings.Contains(err.Error(), "publish outbox message") {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.failureMessageID != message.ID {
		t.Fatalf(
			"expected failure for message %d, got %d",
			message.ID,
			store.failureMessageID,
		)
	}

	if store.failureCode != outboxPublishFailureCode {
		t.Fatalf(
			"expected failure code %q, got %q",
			outboxPublishFailureCode,
			store.failureCode,
		)
	}

	if store.markedPublishedID != 0 {
		t.Fatalf(
			"failed publish unexpectedly marked message %d published",
			store.markedPublishedID,
		)
	}
}

func TestPublishNextOutboxMessageDoesNotHideFailureRecordingError(t *testing.T) {
	message := outboxMessage{
		ID:              31,
		ProcessingJobID: 10,
		EventType:       "work_item.process",
		Payload:         []byte(`{"job_id":10}`),
		PublishAttempts: 1,
	}

	store := &stubOutboxPublisherStore{
		beginMessage: message,
		failureErr:   errors.New("database unavailable"),
	}

	broker := &stubBrokerMessagePublisher{
		publishErr: errors.New("broker unavailable"),
	}

	processed, err := publishNextOutboxMessage(
		context.Background(),
		store,
		broker,
	)
	if err == nil {
		t.Fatal("expected failure-recording error")
	}

	if !processed {
		t.Fatal("expected the selected outbox message to count as processed")
	}

	if !strings.Contains(err.Error(), "record outbox publish failure") {
		t.Fatalf(
			"expected failure-recording context, got: %v",
			err,
		)
	}

	if !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf(
			"expected database failure to be preserved, got: %v",
			err,
		)
	}
}

func TestPublishNextOutboxMessageLeavesMessageRetryableWhenMarkFails(t *testing.T) {
	message := outboxMessage{
		ID:              41,
		ProcessingJobID: 12,
		EventType:       "work_item.process",
		Payload:         []byte(`{"job_id":12}`),
		PublishAttempts: 2,
	}

	store := &stubOutboxPublisherStore{
		beginMessage: message,
		markErr:      errors.New("database unavailable after broker confirm"),
	}

	broker := &stubBrokerMessagePublisher{}

	processed, err := publishNextOutboxMessage(
		context.Background(),
		store,
		broker,
	)
	if err == nil {
		t.Fatal("expected mark-published failure")
	}

	if !processed {
		t.Fatal("expected the selected outbox message to count as processed")
	}

	if broker.publishCalls != 1 {
		t.Fatalf(
			"expected broker publish before database failure, got %d calls",
			broker.publishCalls,
		)
	}

	if store.markedPublishedID != message.ID {
		t.Fatalf(
			"expected mark attempt for message %d, got %d",
			message.ID,
			store.markedPublishedID,
		)
	}

	if store.failureMessageID != 0 {
		t.Fatalf(
			"post-confirm database failure must not be misclassified as broker failure; got message %d",
			store.failureMessageID,
		)
	}

	if !strings.Contains(err.Error(), "mark outbox message published") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishNextOutboxMessagePropagatesStoreSelectionFailure(t *testing.T) {
	store := &stubOutboxPublisherStore{
		beginErr: errors.New("database unavailable"),
	}

	broker := &stubBrokerMessagePublisher{}

	processed, err := publishNextOutboxMessage(
		context.Background(),
		store,
		broker,
	)
	if err == nil {
		t.Fatal("expected outbox selection failure")
	}

	if processed {
		t.Fatal("store selection failure must not report processed work")
	}

	if broker.publishCalls != 0 {
		t.Fatalf(
			"broker was called after store failure: %d",
			broker.publishCalls,
		)
	}

	if !strings.Contains(err.Error(), "begin outbox publish attempt") {
		t.Fatalf("unexpected error: %v", err)
	}
}
