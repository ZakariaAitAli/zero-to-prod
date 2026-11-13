package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/expfmt"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const testTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

var (
	spanRecorderOnce sync.Once
	spanRecorder     *tracetest.SpanRecorder
)

// recordedSpans installs one recording tracer provider for the test binary:
// package tracers bind to the first global provider, so it is set only once.
// Tests find their own spans by name or trace ID.
func recordedSpans() *tracetest.SpanRecorder {
	spanRecorderOnce.Do(func() {
		spanRecorder = tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder)))
	})
	return spanRecorder
}

func endedSpan(t *testing.T, recorder *tracetest.SpanRecorder, match func(sdktrace.ReadOnlySpan) bool) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range recorder.Ended() {
		if match(span) {
			return span
		}
	}
	t.Fatal("expected span was not recorded")
	return nil
}

type traceCapturingStore struct {
	*stubApplicationStore
	acceptTraceParent string
}

func (store *traceCapturingStore) AcceptProcessingJob(ctx context.Context, workItemID int64) (processingJob, error) {
	store.acceptTraceParent = observability.TraceParent(ctx)
	return store.stubApplicationStore.AcceptProcessingJob(ctx, workItemID)
}

func TestInstrumentHTTPLabelsRoutePatterns(t *testing.T) {
	recordedSpans()
	store := &traceCapturingStore{stubApplicationStore: &stubApplicationStore{
		acceptJob: processingJob{ID: 9, WorkItemID: 5, State: "accepted"},
	}}
	handler := instrumentHTTP(newApplicationHandler("test", readyState(), store, withoutAuthentication))

	cases := []struct {
		method, target, route, code string
	}{
		{http.MethodPost, "/items/5/process", "/items/{id}/process", "202"},
		{http.MethodGet, "/items/5/unknown?secret=x", "unmatched", "404"},
		{http.MethodGet, "/health", "/health", "200"},
		{http.MethodDelete, "/items", "unmatched", "405"},
	}
	for _, tc := range cases {
		method := methodLabel(tc.method)
		before := testutil.ToFloat64(httpRequests.WithLabelValues(tc.route, method, tc.code))

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.target, nil))

		if got := testutil.ToFloat64(httpRequests.WithLabelValues(tc.route, method, tc.code)) - before; got != 1 {
			t.Errorf("%s %s: counter for route=%q code=%s grew by %v (status %d)",
				tc.method, tc.target, tc.route, tc.code, got, response.Code)
		}
	}

	// Query strings and IDs never become label values.
	exported, err := testutil.CollectAndFormat(httpRequests, expfmt.TypeTextPlain)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"secret", "/items/5"} {
		if strings.Contains(string(exported), value) {
			t.Errorf("label value containing %q exported:\n%s", value, exported)
		}
	}

	if store.acceptTraceParent == "" {
		t.Fatal("acceptance ran without the request span in its context")
	}
}

func TestInstrumentHTTPStartsNewTraceIgnoringInboundHeader(t *testing.T) {
	recorder := recordedSpans()
	handler := instrumentHTTP(newApplicationHandler("test", readyState(), &stubApplicationStore{}, withoutAuthentication))

	request := httptest.NewRequest(http.MethodGet, "/items", nil)
	request.Header.Set("traceparent", testTraceParent)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	span := endedSpan(t, recorder, func(span sdktrace.ReadOnlySpan) bool {
		return span.Name() == "GET /items"
	})
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("span kind = %v", span.SpanKind())
	}
	if span.Parent().IsValid() || span.SpanContext().TraceID().String() == "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("inbound traceparent was trusted: parent=%v trace=%v", span.Parent(), span.SpanContext().TraceID())
	}
}

type contextCapturingPublisher struct {
	ctx context.Context
	err error
}

func (publisher *contextCapturingPublisher) Publish(ctx context.Context, _ outboxMessage) error {
	publisher.ctx = ctx
	return publisher.err
}

func TestPublishContinuesAcceptanceTrace(t *testing.T) {
	recorder := recordedSpans()
	store := &stubOutboxPublisherStore{beginMessage: outboxMessage{
		ID: 31, ProcessingJobID: 17, EventType: "work_item.process",
		Payload: []byte(`{}`), PublishAttempts: 1, TraceParent: testTraceParent,
	}}
	publisher := &contextCapturingPublisher{}
	before := testutil.ToFloat64(outboxPublishResults.WithLabelValues("published"))

	if _, err := publishNextOutboxMessage(context.Background(), store, publisher); err != nil {
		t.Fatal(err)
	}

	published := trace.SpanContextFromContext(publisher.ctx)
	if published.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("publish trace = %v, want the acceptance trace", published.TraceID())
	}
	span := endedSpan(t, recorder, func(span sdktrace.ReadOnlySpan) bool {
		return span.SpanContext().SpanID() == published.SpanID()
	})
	if span.SpanKind() != trace.SpanKindProducer || span.Parent().SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("publish span kind=%v parent=%v", span.SpanKind(), span.Parent().SpanID())
	}
	if got := testutil.ToFloat64(outboxPublishResults.WithLabelValues("published")) - before; got != 1 {
		t.Fatalf("published counter grew by %v", got)
	}
}

func TestPublishWithoutTraceContextStillPublishes(t *testing.T) {
	recordedSpans()
	store := &stubOutboxPublisherStore{beginMessage: outboxMessage{ID: 32, EventType: "work_item.process"}}
	publisher := &contextCapturingPublisher{err: errors.New("channel closed")}
	before := testutil.ToFloat64(outboxPublishResults.WithLabelValues("broker_error"))

	_, err := publishNextOutboxMessage(context.Background(), store, publisher)
	if !errors.Is(err, errBrokerPublish) {
		t.Fatalf("error = %v, want broker publish failure", err)
	}
	if !trace.SpanContextFromContext(publisher.ctx).IsValid() {
		t.Fatal("publish without stored trace context should start a new trace")
	}
	if got := testutil.ToFloat64(outboxPublishResults.WithLabelValues("broker_error")) - before; got != 1 {
		t.Fatalf("broker_error counter grew by %v", got)
	}
}

func TestRouteAndMethodLabelsAreBounded(t *testing.T) {
	for pattern, want := range map[string]string{
		"":                          "unmatched",
		"GET /processing-jobs/{id}": "/processing-jobs/{id}",
		"/health":                   "/health",
	} {
		if got := routeLabel(pattern); got != want {
			t.Errorf("routeLabel(%q) = %q, want %q", pattern, got, want)
		}
	}
	for method, want := range map[string]string{"GET": "GET", "POST": "POST", "PATCH": "other", "BREW": "other"} {
		if got := methodLabel(method); got != want {
			t.Errorf("methodLabel(%q) = %q, want %q", method, got, want)
		}
	}
}

func readyState() *readinessState {
	state := &readinessState{}
	state.set(true)
	return state
}
