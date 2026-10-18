package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var (
	spanRecorderOnce sync.Once
	spanRecorder     *tracetest.SpanRecorder
)

// recordedSpans installs one recording tracer provider for the test binary:
// package tracers bind to the first global provider, so it is set only once.
func recordedSpans() *tracetest.SpanRecorder {
	spanRecorderOnce.Do(func() {
		spanRecorder = tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder)))
	})
	return spanRecorder
}

func TestDeliverySpanContinuesPublishedTrace(t *testing.T) {
	recorder := recordedSpans()
	events := []string{}
	store := &orderedCompleter{events: &events, result: processingCompletion{Disposition: completionApplied}}
	delivery := validWorkerDelivery(&orderedAcknowledger{events: &events})
	delivery.MessageId = "77"
	delivery.Headers = amqp.Table{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}
	before := testutil.ToFloat64(workerDeliveries.WithLabelValues("ack", "false"))

	if _, err := processRabbitMQDelivery(context.Background(), store, delivery); err != nil {
		t.Fatal(err)
	}

	var span sdktrace.ReadOnlySpan
	for _, ended := range recorder.Ended() {
		if ended.SpanContext().TraceID().String() == "0af7651916cd43dd8448eb211c80319c" {
			span = ended
		}
	}
	if span == nil {
		t.Fatal("no delivery span in the published trace")
	}
	if span.SpanKind() != trace.SpanKindConsumer || span.Parent().SpanID().String() != "b7ad6b7169203331" {
		t.Fatalf("span kind=%v parent=%v", span.SpanKind(), span.Parent().SpanID())
	}
	attributes := map[string]any{}
	for _, attribute := range span.Attributes() {
		attributes[string(attribute.Key)] = attribute.Value.AsInterface()
	}
	if attributes["work_items.job_id"] != int64(101) || attributes["work_items.settlement"] != "ack" {
		t.Fatalf("span attributes = %v", attributes)
	}
	if got := testutil.ToFloat64(workerDeliveries.WithLabelValues("ack", "false")) - before; got != 1 {
		t.Fatalf("ack counter grew by %v", got)
	}
}

func TestDeliveryWithoutTraceHeaderStartsNewTrace(t *testing.T) {
	recordedSpans()
	events := []string{}
	store := &orderedCompleter{events: &events, result: processingCompletion{Disposition: completionApplied}}

	result, err := processRabbitMQDelivery(context.Background(), store, validWorkerDelivery(&orderedAcknowledger{events: &events}))
	if err != nil || result.Settlement != settlementAck {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type closableCompleter struct {
	*orderedCompleter
}

func (closableCompleter) Close() {}

func TestInstrumentedStoreObservesCompletionLatency(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(workerCompletionLatency)
	defer registry.Unregister(workerCompletionLatency)

	finished := workerJobRowAcceptedAt.Add(1500 * time.Millisecond)
	events := []string{}
	completer := &orderedCompleter{
		events: &events,
		result: processingCompletion{
			Disposition: completionApplied,
			Job:         workerProcessingJob{FinishedAt: &finished},
			AcceptedAt:  workerJobRowAcceptedAt,
		},
	}
	store := instrumentedWorkerStore{closableCompleter{completer}}
	before := histogramState(t, registry)
	beforeCompleted := testutil.ToFloat64(workerCompletions.WithLabelValues(string(completionApplied)))

	if _, err := store.CompleteProcessingJob(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}

	after := histogramState(t, registry)
	if after.count-before.count != 1 || after.sum-before.sum != 1.5 {
		t.Fatalf("latency histogram delta count=%d sum=%v", after.count-before.count, after.sum-before.sum)
	}
	if got := testutil.ToFloat64(workerCompletions.WithLabelValues(string(completionApplied))) - beforeCompleted; got != 1 {
		t.Fatalf("completions grew by %v", got)
	}

	// A duplicate delivery of a finished job is counted but adds no latency.
	completer.result = processingCompletion{Disposition: completionAlreadyTerminal}
	if _, err := store.CompleteProcessingJob(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	if final := histogramState(t, registry); final.count != after.count {
		t.Fatal("duplicate completion observed a latency")
	}
}

func TestInstrumentedStoreCountsFailureDispositions(t *testing.T) {
	events := []string{}
	completer := &orderedCompleter{events: &events}
	store := instrumentedWorkerStore{closableCompleter{completer}}

	for _, disposition := range []processingFailureDisposition{
		processingFailureRetryable,
		processingFailureTerminal,
		processingFailureAlreadyTerminal,
	} {
		completer.failureResult = processingFailure{Disposition: disposition}
		before := testutil.ToFloat64(workerProcessingFailures.WithLabelValues(string(disposition)))

		if _, err := store.RecordProcessingFailure(context.Background(), 1, 2, workerProcessingFailureCode, workerProcessingMaxAttempts); err != nil {
			t.Fatal(err)
		}
		if got := testutil.ToFloat64(workerProcessingFailures.WithLabelValues(string(disposition))) - before; got != 1 {
			t.Errorf("%s failures grew by %v", disposition, got)
		}
	}

	// A failure that could not be recorded is not counted.
	completer.failureErr = errors.New("database unavailable")
	before := testutil.ToFloat64(workerProcessingFailures.WithLabelValues(string(processingFailureAlreadyTerminal)))
	if _, err := store.RecordProcessingFailure(context.Background(), 1, 2, workerProcessingFailureCode, workerProcessingMaxAttempts); err == nil {
		t.Fatal("expected the store error")
	}
	if got := testutil.ToFloat64(workerProcessingFailures.WithLabelValues(string(processingFailureAlreadyTerminal))) - before; got != 0 {
		t.Errorf("unrecorded failure counted: %v", got)
	}
}

type histogramSnapshot struct {
	count uint64
	sum   float64
}

func histogramState(t *testing.T, registry *prometheus.Registry) histogramSnapshot {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "work_items_worker_job_completion_latency_seconds" {
			histogram := family.GetMetric()[0].GetHistogram()
			return histogramSnapshot{count: histogram.GetSampleCount(), sum: histogram.GetSampleSum()}
		}
	}
	return histogramSnapshot{}
}
