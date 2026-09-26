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

	workerProcessingFailureCode = "processing_failed"
	workerProcessingMaxAttempts = 3
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
	GetProcessingJob(
		context.Context,
		int64,
		int64,
	) (workerProcessingJob, error)

	CompleteProcessingJob(
		context.Context,
		int64,
		int64,
	) (processingCompletion, error)

	RecordProcessingFailure(
		context.Context,
		int64,
		int64,
		string,
		int,
	) (processingFailure, error)
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
	return handleWorkerMessageWithProcessor(
		ctx,
		store,
		processingJobProcessorFunc(
			successfulProcessingJobProcessor,
		),
		payload,
	)
}

func handleWorkerMessageWithProcessor(
	ctx context.Context,
	store processingJobCompleter,
	processor processingJobProcessor,
	payload []byte,
) (deliverySettlement, error) {
	message, err := decodeWorkerMessage(payload)
	if err != nil {
		return settlementReject, err
	}

	if err := validateWorkerMessage(message); err != nil {
		return settlementReject, err
	}

	job, err := store.GetProcessingJob(
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
				"processing job inspection unavailable: %w",
				err,
			)
		}
	}

	switch job.State {
	case "succeeded", "failed":
		return settlementAck, nil

	case "accepted":
		// Continue with the processing attempt.

	default:
		return settlementNackRequeue, fmt.Errorf(
			"%w: job_id=%d unexpected_state=%q",
			errProcessingJobConflict,
			message.JobID,
			job.State,
		)
	}

	processingErr := processor.Process(
		ctx,
		message,
	)
	if processingErr != nil {
		failure, failureErr := store.RecordProcessingFailure(
			ctx,
			message.JobID,
			message.WorkItemID,
			workerProcessingFailureCode,
			workerProcessingMaxAttempts,
		)
		if failureErr != nil {
			switch {
			case errors.Is(
				failureErr,
				errProcessingJobNotFound,
			):
				return settlementReject, errors.Join(
					fmt.Errorf(
						"process work item job: %w",
						processingErr,
					),
					fmt.Errorf(
						"reject unknown processing job while recording failure: %w",
						failureErr,
					),
				)

			case errors.Is(
				failureErr,
				errProcessingJobMismatch,
			):
				return settlementReject, errors.Join(
					fmt.Errorf(
						"process work item job: %w",
						processingErr,
					),
					fmt.Errorf(
						"reject processing job identity mismatch while recording failure: %w",
						failureErr,
					),
				)

			default:
				return settlementNackRequeue, errors.Join(
					fmt.Errorf(
						"process work item job: %w",
						processingErr,
					),
					fmt.Errorf(
						"record processing failure: %w",
						failureErr,
					),
				)
			}
		}

		switch failure.Disposition {
		case processingFailureRetryable:
			return settlementNackRequeue, fmt.Errorf(
				"process work item job: %w",
				processingErr,
			)

		case processingFailureTerminal,
			processingFailureAlreadyTerminal:
			return settlementAck, nil

		default:
			return settlementNackRequeue, fmt.Errorf(
				"unexpected processing failure disposition: %q",
				failure.Disposition,
			)
		}
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
