package main

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
)

// Labels are bounded (ADR 0012): settlement is ack, reject_no_requeue,
// nack_requeue, or settle_failed; redelivered is true or false; disposition
// is one of the store's fixed dispositions.

var workerTracer = otel.Tracer("github.com/ZakariaAitAli/zero-to-prod/apps/work-items/worker")

var (
	workerDeliveries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "work_items_worker_deliveries_total",
		Help: "Deliveries handled, by settlement and broker redelivery flag.",
	}, []string{"settlement", "redelivered"})

	workerDeliveryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "work_items_worker_delivery_duration_seconds",
		Help:    "Time to handle and settle one delivery, by settlement.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"settlement"})

	workerCompletions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "work_items_worker_job_completions_total",
		Help: "Completion results: completed_now, or already_terminal for a duplicate.",
	}, []string{"disposition"})

	// 30 s is the asynchronous freshness SLO threshold; keep it a bucket boundary.
	workerCompletionLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "work_items_worker_job_completion_latency_seconds",
		Help:    "Time from acceptance to completion for jobs this worker completed (database clock).",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900},
	})

	workerProcessingFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "work_items_worker_processing_failures_total",
		Help: "Recorded processor failures by disposition: retryable, terminal, or already_terminal.",
	}, []string{"disposition"})

	workerSessionExits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "work_items_worker_session_exits_total",
		Help: "Broker sessions that ended and were retried (requeue, broker or store failure).",
	})
)

func registerWorkerMetrics(registry prometheus.Registerer) {
	registry.MustRegister(
		workerDeliveries,
		workerDeliveryDuration,
		workerCompletions,
		workerCompletionLatency,
		workerProcessingFailures,
		workerSessionExits,
	)
}

// instrumentedWorkerStore records completion and failure outcomes without
// changing how they are decided.
type instrumentedWorkerStore struct {
	workerRuntimeStore
}

func (store instrumentedWorkerStore) CompleteProcessingJob(
	ctx context.Context,
	jobID int64,
	workItemID int64,
) (processingCompletion, error) {
	completion, err := store.workerRuntimeStore.CompleteProcessingJob(ctx, jobID, workItemID)
	if err != nil {
		return completion, err
	}

	workerCompletions.WithLabelValues(string(completion.Disposition)).Inc()
	if completion.Disposition == completionApplied &&
		completion.Job.FinishedAt != nil &&
		!completion.AcceptedAt.IsZero() {
		workerCompletionLatency.Observe(completion.Job.FinishedAt.Sub(completion.AcceptedAt).Seconds())
	}
	return completion, nil
}

func (store instrumentedWorkerStore) RecordProcessingFailure(
	ctx context.Context,
	jobID int64,
	workItemID int64,
	errorCode string,
	maxAttempts int,
) (processingFailure, error) {
	failure, err := store.workerRuntimeStore.RecordProcessingFailure(ctx, jobID, workItemID, errorCode, maxAttempts)
	if err == nil {
		workerProcessingFailures.WithLabelValues(string(failure.Disposition)).Inc()
	}
	return failure, err
}
