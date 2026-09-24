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
