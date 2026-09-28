package main

import (
	"context"
	"errors"
	"testing"
)

type stubProcessingJobCompleter struct {
	result processingCompletion
	err    error

	calls      int
	jobID      int64
	workItemID int64

	inspectJob   workerProcessingJob
	inspectErr   error
	inspectCalls int

	failureResult processingFailure
	failureErr    error
	failureCalls  int
	failureCode   string
	maxAttempts   int
}

func (store *stubProcessingJobCompleter) GetProcessingJob(
	_ context.Context,
	jobID int64,
	workItemID int64,
) (workerProcessingJob, error) {
	store.inspectCalls++

	if store.inspectErr != nil {
		return workerProcessingJob{}, store.inspectErr
	}

	if store.inspectJob.ID != 0 {
		return store.inspectJob, nil
	}

	return workerProcessingJob{
		ID:         jobID,
		WorkItemID: workItemID,
		State:      "accepted",
	}, nil
}

func (store *stubProcessingJobCompleter) CompleteProcessingJob(
	_ context.Context,
	jobID int64,
	workItemID int64,
) (processingCompletion, error) {
	store.calls++
	store.jobID = jobID
	store.workItemID = workItemID

	return store.result, store.err
}

func (store *stubProcessingJobCompleter) RecordProcessingFailure(
	_ context.Context,
	jobID int64,
	workItemID int64,
	errorCode string,
	maxAttempts int,
) (processingFailure, error) {
	store.failureCalls++
	store.failureCode = errorCode
	store.maxAttempts = maxAttempts

	if store.failureErr != nil {
		return processingFailure{}, store.failureErr
	}

	if store.failureResult.Disposition != "" {
		return store.failureResult, nil
	}

	return processingFailure{
		Disposition: processingFailureRetryable,
		Job: workerProcessingJob{
			ID:           jobID,
			WorkItemID:   workItemID,
			State:        "accepted",
			AttemptCount: 1,
		},
	}, nil
}

func TestHandleWorkerMessageAcksNewCompletion(
	t *testing.T,
) {
	store := &stubProcessingJobCompleter{
		result: processingCompletion{
			Disposition: completionApplied,
		},
	}

	settlement, err := handleWorkerMessage(
		context.Background(),
		store,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":101,
			"work_item_id":201
		}`),
	)
	if err != nil {
		t.Fatalf("handle valid worker message: %v", err)
	}

	if settlement != settlementAck {
		t.Fatalf(
			"expected %q, got %q",
			settlementAck,
			settlement,
		)
	}

	if store.calls != 1 {
		t.Fatalf(
			"expected one completion call, got %d",
			store.calls,
		)
	}

	if store.jobID != 101 || store.workItemID != 201 {
		t.Fatalf(
			"unexpected completion identity: job=%d work_item=%d",
			store.jobID,
			store.workItemID,
		)
	}
}

func TestHandleWorkerMessageAcksTerminalRedelivery(
	t *testing.T,
) {
	store := &stubProcessingJobCompleter{
		result: processingCompletion{
			Disposition: completionAlreadyTerminal,
		},
	}

	settlement, err := handleWorkerMessage(
		context.Background(),
		store,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":301,
			"work_item_id":401
		}`),
	)
	if err != nil {
		t.Fatalf("handle terminal redelivery: %v", err)
	}

	if settlement != settlementAck {
		t.Fatalf(
			"expected terminal redelivery to ACK, got %q",
			settlement,
		)
	}
}

func TestHandleWorkerMessageRejectsMalformedPayload(
	t *testing.T,
) {
	store := &stubProcessingJobCompleter{}

	settlement, err := handleWorkerMessage(
		context.Background(),
		store,
		[]byte(`{"type":`),
	)

	if settlement != settlementReject {
		t.Fatalf(
			"expected malformed payload to reject, got %q",
			settlement,
		)
	}

	if err == nil {
		t.Fatal("expected malformed payload error")
	}

	if store.calls != 0 {
		t.Fatalf(
			"malformed payload unexpectedly touched database %d times",
			store.calls,
		)
	}
}

func TestHandleWorkerMessageRejectsUnsupportedContract(
	t *testing.T,
) {
	testCases := []struct {
		name    string
		payload string
	}{
		{
			name: "wrong type",
			payload: `{
				"type":"other.event",
				"version":1,
				"job_id":1,
				"work_item_id":2
			}`,
		},
		{
			name: "wrong version",
			payload: `{
				"type":"work_item.process",
				"version":2,
				"job_id":1,
				"work_item_id":2
			}`,
		},
		{
			name: "invalid job id",
			payload: `{
				"type":"work_item.process",
				"version":1,
				"job_id":0,
				"work_item_id":2
			}`,
		},
		{
			name: "invalid work item id",
			payload: `{
				"type":"work_item.process",
				"version":1,
				"job_id":1,
				"work_item_id":0
			}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &stubProcessingJobCompleter{}

			settlement, err := handleWorkerMessage(
				context.Background(),
				store,
				[]byte(testCase.payload),
			)

			if settlement != settlementReject {
				t.Fatalf(
					"expected invalid contract to reject, got %q",
					settlement,
				)
			}

			if err == nil {
				t.Fatal("expected invalid contract error")
			}

			if store.calls != 0 {
				t.Fatalf(
					"invalid contract unexpectedly touched database %d times",
					store.calls,
				)
			}
		})
	}
}

func TestHandleWorkerMessageRejectsPermanentIdentityErrors(
	t *testing.T,
) {
	testCases := []struct {
		name string
		err  error
	}{
		{
			name: "unknown job",
			err:  errProcessingJobNotFound,
		},
		{
			name: "work item mismatch",
			err:  errProcessingJobMismatch,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &stubProcessingJobCompleter{
				err: testCase.err,
			}

			settlement, err := handleWorkerMessage(
				context.Background(),
				store,
				[]byte(`{
					"type":"work_item.process",
					"version":1,
					"job_id":501,
					"work_item_id":601
				}`),
			)

			if settlement != settlementReject {
				t.Fatalf(
					"expected permanent identity error to reject, got %q",
					settlement,
				)
			}

			if !errors.Is(err, testCase.err) {
				t.Fatalf(
					"expected original error %v, got %v",
					testCase.err,
					err,
				)
			}
		})
	}
}

func TestHandleWorkerMessageRequeuesTransientStoreFailure(
	t *testing.T,
) {
	storeErr := errors.New("PostgreSQL temporarily unavailable")

	store := &stubProcessingJobCompleter{
		err: storeErr,
	}

	settlement, err := handleWorkerMessage(
		context.Background(),
		store,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":701,
			"work_item_id":801
		}`),
	)

	if settlement != settlementNackRequeue {
		t.Fatalf(
			"expected transient store failure to requeue, got %q",
			settlement,
		)
	}

	if !errors.Is(err, storeErr) {
		t.Fatalf(
			"expected original store error, got %v",
			err,
		)
	}
}

func TestHandleWorkerMessageRequeuesCompletionConflict(
	t *testing.T,
) {
	store := &stubProcessingJobCompleter{
		err: errProcessingJobConflict,
	}

	settlement, err := handleWorkerMessage(
		context.Background(),
		store,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":901,
			"work_item_id":1001
		}`),
	)

	if settlement != settlementNackRequeue {
		t.Fatalf(
			"expected completion conflict to requeue, got %q",
			settlement,
		)
	}

	if !errors.Is(err, errProcessingJobConflict) {
		t.Fatalf(
			"expected conflict error, got %v",
			err,
		)
	}
}

type stubProcessingJobProcessor struct {
	err error

	calls      int
	jobID      int64
	workItemID int64
}

func (processor *stubProcessingJobProcessor) Process(
	_ context.Context,
	message workerMessage,
) error {
	processor.calls++
	processor.jobID = message.JobID
	processor.workItemID = message.WorkItemID

	return processor.err
}

func TestHandleWorkerMessageRunsProcessorBeforeDurableCompletion(
	t *testing.T,
) {
	processor := &stubProcessingJobProcessor{}

	store := &stubProcessingJobCompleter{
		result: processingCompletion{
			Disposition: completionApplied,
		},
	}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":1101,
			"work_item_id":1201
		}`),
	)
	if err != nil {
		t.Fatalf(
			"handle valid worker message with processor: %v",
			err,
		)
	}

	if settlement != settlementAck {
		t.Fatalf(
			"expected %q, got %q",
			settlementAck,
			settlement,
		)
	}

	if processor.calls != 1 {
		t.Fatalf(
			"expected one processing attempt, got %d",
			processor.calls,
		)
	}

	if processor.jobID != 1101 ||
		processor.workItemID != 1201 {
		t.Fatalf(
			"unexpected processor identity: job=%d work_item=%d",
			processor.jobID,
			processor.workItemID,
		)
	}

	if store.calls != 1 {
		t.Fatalf(
			"expected durable completion after processor success, got %d calls",
			store.calls,
		)
	}
}

func TestHandleWorkerMessageRequeuesProcessingFailureWithoutClaimingCompletion(
	t *testing.T,
) {
	processingErr := errors.New(
		"representative processing temporarily failed",
	)

	processor := &stubProcessingJobProcessor{
		err: processingErr,
	}

	store := &stubProcessingJobCompleter{}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":1301,
			"work_item_id":1401
		}`),
	)

	if settlement != settlementNackRequeue {
		t.Fatalf(
			"expected processing failure to request requeue, got %q",
			settlement,
		)
	}

	if !errors.Is(err, processingErr) {
		t.Fatalf(
			"expected original processing error %v, got %v",
			processingErr,
			err,
		)
	}

	if processor.calls != 1 {
		t.Fatalf(
			"expected one processing attempt, got %d",
			processor.calls,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"processing failure falsely attempted durable success %d times",
			store.calls,
		)
	}
}

func TestHandleWorkerMessageSkipsProcessorForTerminalJob(
	t *testing.T,
) {
	processor := &stubProcessingJobProcessor{
		err: errors.New(
			"processor must not run for terminal job",
		),
	}

	store := &stubProcessingJobCompleter{
		inspectJob: workerProcessingJob{
			ID:           1501,
			WorkItemID:   1601,
			State:        "failed",
			AttemptCount: workerProcessingMaxAttempts,
		},
	}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":1501,
			"work_item_id":1601
		}`),
	)
	if err != nil {
		t.Fatalf(
			"terminal redelivery returned error: %v",
			err,
		)
	}

	if settlement != settlementAck {
		t.Fatalf(
			"expected terminal redelivery ACK, got %q",
			settlement,
		)
	}

	if processor.calls != 0 {
		t.Fatalf(
			"terminal job was processed %d times",
			processor.calls,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"terminal job attempted completion %d times",
			store.calls,
		)
	}

	if store.failureCalls != 0 {
		t.Fatalf(
			"terminal job recorded failure %d times",
			store.failureCalls,
		)
	}
}

func TestHandleWorkerMessageRejectsUnknownJobBeforeProcessor(
	t *testing.T,
) {
	processor := &stubProcessingJobProcessor{}

	store := &stubProcessingJobCompleter{
		inspectErr: errProcessingJobNotFound,
	}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":1701,
			"work_item_id":1801
		}`),
	)

	if settlement != settlementReject {
		t.Fatalf(
			"expected unknown job reject, got %q",
			settlement,
		)
	}

	if !errors.Is(
		err,
		errProcessingJobNotFound,
	) {
		t.Fatalf(
			"expected not-found error, got %v",
			err,
		)
	}

	if processor.calls != 0 {
		t.Fatalf(
			"unknown job reached processor %d times",
			processor.calls,
		)
	}
}

func TestHandleWorkerMessageRecordsRetryableFailureBeforeRequeue(
	t *testing.T,
) {
	processingErr := errors.New(
		"representative processing failure",
	)

	processor := &stubProcessingJobProcessor{
		err: processingErr,
	}

	store := &stubProcessingJobCompleter{
		failureResult: processingFailure{
			Disposition: processingFailureRetryable,
			Job: workerProcessingJob{
				ID:           1901,
				WorkItemID:   2001,
				State:        "accepted",
				AttemptCount: 1,
			},
		},
	}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":1901,
			"work_item_id":2001
		}`),
	)

	if settlement != settlementNackRequeue {
		t.Fatalf(
			"expected retryable failure to requeue, got %q",
			settlement,
		)
	}

	if !errors.Is(err, processingErr) {
		t.Fatalf(
			"expected processing error %v, got %v",
			processingErr,
			err,
		)
	}

	if store.failureCalls != 1 {
		t.Fatalf(
			"expected one durable failure record, got %d",
			store.failureCalls,
		)
	}

	if store.failureCode != workerProcessingFailureCode {
		t.Fatalf(
			"expected stable failure code %q, got %q",
			workerProcessingFailureCode,
			store.failureCode,
		)
	}

	if store.maxAttempts != workerProcessingMaxAttempts {
		t.Fatalf(
			"expected max attempts %d, got %d",
			workerProcessingMaxAttempts,
			store.maxAttempts,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"retryable processing failure falsely completed job %d times",
			store.calls,
		)
	}
}

func TestHandleWorkerMessageAcksDurableTerminalFailure(
	t *testing.T,
) {
	processingErr := errors.New(
		"representative processing failure",
	)

	processor := &stubProcessingJobProcessor{
		err: processingErr,
	}

	store := &stubProcessingJobCompleter{
		failureResult: processingFailure{
			Disposition: processingFailureTerminal,
			Job: workerProcessingJob{
				ID:           2101,
				WorkItemID:   2201,
				State:        "failed",
				AttemptCount: workerProcessingMaxAttempts,
			},
		},
	}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":2101,
			"work_item_id":2201
		}`),
	)
	if err != nil {
		t.Fatalf(
			"durably exhausted failure returned error: %v",
			err,
		)
	}

	if settlement != settlementAck {
		t.Fatalf(
			"expected terminal failure ACK, got %q",
			settlement,
		)
	}

	if processor.calls != 1 {
		t.Fatalf(
			"expected one processing attempt, got %d",
			processor.calls,
		)
	}

	if store.failureCalls != 1 {
		t.Fatalf(
			"expected one durable failure record, got %d",
			store.failureCalls,
		)
	}

	if store.calls != 0 {
		t.Fatalf(
			"terminal processing failure falsely completed job %d times",
			store.calls,
		)
	}
}

func TestHandleWorkerMessageRequeuesWhenFailureRecordIsUnavailable(
	t *testing.T,
) {
	processingErr := errors.New(
		"representative processing failure",
	)
	databaseErr := errors.New(
		"PostgreSQL temporarily unavailable",
	)

	processor := &stubProcessingJobProcessor{
		err: processingErr,
	}

	store := &stubProcessingJobCompleter{
		failureErr: databaseErr,
	}

	settlement, err := handleWorkerMessageWithProcessor(
		context.Background(),
		store,
		processor,
		[]byte(`{
			"type":"work_item.process",
			"version":1,
			"job_id":2301,
			"work_item_id":2401
		}`),
	)

	if settlement != settlementNackRequeue {
		t.Fatalf(
			"expected unavailable failure record to requeue, got %q",
			settlement,
		)
	}

	if !errors.Is(err, processingErr) {
		t.Fatalf(
			"combined error lost processing error %v: %v",
			processingErr,
			err,
		)
	}

	if !errors.Is(err, databaseErr) {
		t.Fatalf(
			"combined error lost database error %v: %v",
			databaseErr,
			err,
		)
	}

	if store.failureCalls != 1 {
		t.Fatalf(
			"expected one attempted failure record, got %d",
			store.failureCalls,
		)
	}
}
