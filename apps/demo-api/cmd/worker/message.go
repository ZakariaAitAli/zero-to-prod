package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	workItemProcessMessageType    = "work_item.process"
	workItemProcessMessageVersion = 1
)

type workerMessage struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	JobID      int64  `json:"job_id"`
	WorkItemID int64  `json:"work_item_id"`
}

type deliverySettlement string

const (
	settlementAck         deliverySettlement = "ack"
	settlementReject      deliverySettlement = "reject_no_requeue"
	settlementNackRequeue deliverySettlement = "nack_requeue"
)

type processingJobCompleter interface {
	CompleteProcessingJob(
		context.Context,
		int64,
		int64,
	) (processingCompletion, error)
}

func decodeWorkerMessage(
	payload []byte,
) (workerMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	var message workerMessage

	if err := decoder.Decode(&message); err != nil {
		return workerMessage{}, fmt.Errorf(
			"decode worker message: %w",
			err,
		)
	}

	var trailing any

	err := decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			return workerMessage{}, errors.New(
				"decode worker message: trailing JSON value",
			)
		}

		return workerMessage{}, fmt.Errorf(
			"decode worker message trailing data: %w",
			err,
		)
	}

	return message, nil
}

func validateWorkerMessage(
	message workerMessage,
) error {
	if message.Type != workItemProcessMessageType {
		return fmt.Errorf(
			"unsupported worker message type: %q",
			message.Type,
		)
	}

	if message.Version != workItemProcessMessageVersion {
		return fmt.Errorf(
			"unsupported worker message version: %d",
			message.Version,
		)
	}

	if message.JobID <= 0 {
		return fmt.Errorf(
			"invalid worker message job_id: %d",
			message.JobID,
		)
	}

	if message.WorkItemID <= 0 {
		return fmt.Errorf(
			"invalid worker message work_item_id: %d",
			message.WorkItemID,
		)
	}

	return nil
}

func handleWorkerMessage(
	ctx context.Context,
	store processingJobCompleter,
	payload []byte,
) (deliverySettlement, error) {
	message, err := decodeWorkerMessage(payload)
	if err != nil {
		return settlementReject, err
	}

	if err := validateWorkerMessage(message); err != nil {
		return settlementReject, err
	}

	completion, err := store.CompleteProcessingJob(
		ctx,
		message.JobID,
		message.WorkItemID,
	)
	if err != nil {
		switch {
		case errors.Is(err, errProcessingJobNotFound):
			return settlementReject, fmt.Errorf(
				"reject unknown processing job: %w",
				err,
			)

		case errors.Is(err, errProcessingJobMismatch):
			return settlementReject, fmt.Errorf(
				"reject processing job identity mismatch: %w",
				err,
			)

		default:
			return settlementNackRequeue, fmt.Errorf(
				"processing job completion unavailable: %w",
				err,
			)
		}
	}

	switch completion.Disposition {
	case completionApplied:
		return settlementAck, nil

	case completionAlreadyTerminal:
		return settlementAck, nil

	default:
		return settlementNackRequeue, fmt.Errorf(
			"unexpected processing completion disposition: %q",
			completion.Disposition,
		)
	}
}
