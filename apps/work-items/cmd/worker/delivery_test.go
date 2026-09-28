package main

import (
	"errors"
	"fmt"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type acknowledgementCall struct {
	operation string
	tag       uint64
	multiple  bool
	requeue   bool
}

type stubAcknowledger struct {
	calls []acknowledgementCall
	err   error
}

func (ack *stubAcknowledger) Ack(
	tag uint64,
	multiple bool,
) error {
	ack.calls = append(
		ack.calls,
		acknowledgementCall{
			operation: "ack",
			tag:       tag,
			multiple:  multiple,
		},
	)

	return ack.err
}

func (ack *stubAcknowledger) Reject(
	tag uint64,
	requeue bool,
) error {
	ack.calls = append(
		ack.calls,
		acknowledgementCall{
			operation: "reject",
			tag:       tag,
			requeue:   requeue,
		},
	)

	return ack.err
}

func (ack *stubAcknowledger) Nack(
	tag uint64,
	multiple bool,
	requeue bool,
) error {
	ack.calls = append(
		ack.calls,
		acknowledgementCall{
			operation: "nack",
			tag:       tag,
			multiple:  multiple,
			requeue:   requeue,
		},
	)

	return ack.err
}

func workerTestDelivery(
	ack amqp.Acknowledger,
) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  42,
	}
}

func TestSettleRabbitMQDeliveryAcksOnlyCurrentDelivery(
	t *testing.T,
) {
	ack := &stubAcknowledger{}

	err := settleRabbitMQDelivery(
		workerTestDelivery(ack),
		settlementAck,
	)
	if err != nil {
		t.Fatalf("settle ACK: %v", err)
	}

	if len(ack.calls) != 1 {
		t.Fatalf(
			"expected one acknowledgement call, got %d",
			len(ack.calls),
		)
	}

	call := ack.calls[0]

	if call.operation != "ack" {
		t.Fatalf(
			"expected ACK operation, got %q",
			call.operation,
		)
	}

	if call.tag != 42 {
		t.Fatalf(
			"expected delivery tag 42, got %d",
			call.tag,
		)
	}

	if call.multiple {
		t.Fatal("ACK unexpectedly used multiple=true")
	}
}

func TestSettleRabbitMQDeliveryRejectsWithoutRequeue(
	t *testing.T,
) {
	ack := &stubAcknowledger{}

	err := settleRabbitMQDelivery(
		workerTestDelivery(ack),
		settlementReject,
	)
	if err != nil {
		t.Fatalf("settle reject: %v", err)
	}

	if len(ack.calls) != 1 {
		t.Fatalf(
			"expected one acknowledgement call, got %d",
			len(ack.calls),
		)
	}

	call := ack.calls[0]

	if call.operation != "reject" {
		t.Fatalf(
			"expected reject operation, got %q",
			call.operation,
		)
	}

	if call.requeue {
		t.Fatal("permanent reject unexpectedly requeued message")
	}
}

func TestSettleRabbitMQDeliveryNacksCurrentDeliveryWithRequeue(
	t *testing.T,
) {
	ack := &stubAcknowledger{}

	err := settleRabbitMQDelivery(
		workerTestDelivery(ack),
		settlementNackRequeue,
	)
	if err != nil {
		t.Fatalf("settle NACK: %v", err)
	}

	if len(ack.calls) != 1 {
		t.Fatalf(
			"expected one acknowledgement call, got %d",
			len(ack.calls),
		)
	}

	call := ack.calls[0]

	if call.operation != "nack" {
		t.Fatalf(
			"expected NACK operation, got %q",
			call.operation,
		)
	}

	if call.multiple {
		t.Fatal("NACK unexpectedly used multiple=true")
	}

	if !call.requeue {
		t.Fatal("transient failure NACK did not request requeue")
	}
}

func TestSettleRabbitMQDeliveryPropagatesBrokerSettlementFailure(
	t *testing.T,
) {
	brokerErr := errors.New("AMQP channel unavailable")

	testCases := []struct {
		name       string
		settlement deliverySettlement
	}{
		{
			name:       "ack failure",
			settlement: settlementAck,
		},
		{
			name:       "reject failure",
			settlement: settlementReject,
		},
		{
			name:       "nack failure",
			settlement: settlementNackRequeue,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ack := &stubAcknowledger{
				err: brokerErr,
			}

			err := settleRabbitMQDelivery(
				workerTestDelivery(ack),
				testCase.settlement,
			)

			if !errors.Is(err, brokerErr) {
				t.Fatalf(
					"expected broker settlement error %v, got %v",
					brokerErr,
					err,
				)
			}

			if len(ack.calls) != 1 {
				t.Fatalf(
					"expected one attempted settlement call, got %d",
					len(ack.calls),
				)
			}
		})
	}
}

func TestSettleRabbitMQDeliveryRejectsUnknownSettlementWithoutBrokerCall(
	t *testing.T,
) {
	ack := &stubAcknowledger{}

	err := settleRabbitMQDelivery(
		workerTestDelivery(ack),
		deliverySettlement("unexpected"),
	)
	if err == nil {
		t.Fatal("expected unknown settlement error")
	}

	if len(ack.calls) != 0 {
		t.Fatalf(
			"unknown settlement unexpectedly called broker %d times",
			len(ack.calls),
		)
	}

	expected := `unknown delivery settlement "unexpected"`

	if !errors.Is(
		fmt.Errorf("%w", err),
		err,
	) {
		t.Fatal("unexpected error wrapping behavior")
	}

	if err.Error() == "" ||
		!containsText(err.Error(), expected) {
		t.Fatalf(
			"expected error containing %q, got %q",
			expected,
			err.Error(),
		)
	}
}

func containsText(
	value string,
	expected string,
) bool {
	if len(expected) == 0 {
		return true
	}

	if len(value) < len(expected) {
		return false
	}

	for i := 0; i <= len(value)-len(expected); i++ {
		if value[i:i+len(expected)] == expected {
			return true
		}
	}

	return false
}
