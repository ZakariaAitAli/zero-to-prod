package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/log/logtest"
	"go.opentelemetry.io/otel/trace"
)

func testSpanContext(t *testing.T) trace.SpanContext {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatal(err)
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
}

func TestLoggerAddsTraceAndServiceFields(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "work-items-api", "abc123", slog.LevelInfo)
	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(t))

	logger.InfoContext(ctx, "processing accepted", "job_id", int64(7))

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("log record is not JSON: %v: %s", err, output.String())
	}
	for key, want := range map[string]any{
		"msg":      "processing accepted",
		"service":  "work-items-api",
		"version":  "abc123",
		"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id":  "00f067aa0ba902b7",
		"job_id":   float64(7),
	} {
		if record[key] != want {
			t.Errorf("%s = %v, want %v", key, record[key], want)
		}
	}
}

func TestLoggerOmitsTraceFieldsWithoutSpan(t *testing.T) {
	var output bytes.Buffer
	NewLogger(&output, "work-items-worker", "dev", slog.LevelInfo).
		With("component", "session").
		InfoContext(context.Background(), "started")

	if strings.Contains(output.String(), "trace_id") {
		t.Fatalf("unexpected trace_id without a span: %s", output.String())
	}
	if !strings.Contains(output.String(), `"component":"session"`) {
		t.Fatalf("WithAttrs lost on wrapped handler: %s", output.String())
	}
}

func TestExportingLoggerWritesBothOutputsAtSameLevel(t *testing.T) {
	var output bytes.Buffer
	recorder := logtest.NewRecorder()
	logger := newExportingLogger(&output, "work-items-api", "abc123", slog.LevelInfo, recorder)
	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(t))

	logger.DebugContext(ctx, "suppressed")
	logger.InfoContext(ctx, "delivery settled", "job_id", int64(7))

	if lines := strings.Count(output.String(), "\n"); lines != 1 || !strings.Contains(output.String(), `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`) {
		t.Fatalf("stdout = %s", output.String())
	}

	var exported []logtest.Record
	for _, records := range recorder.Result() {
		exported = append(exported, records...)
	}
	if len(exported) != 1 {
		t.Fatalf("exported %d records, want 1: %+v", len(exported), exported)
	}
	if exported[0].Body.AsString() != "delivery settled" {
		t.Fatalf("exported body = %v", exported[0].Body)
	}
	if traceID := trace.SpanContextFromContext(exported[0].Context).TraceID(); traceID.String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("exported record lost trace context: %v", traceID)
	}
}

func TestTraceParentRoundTrip(t *testing.T) {
	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(t))

	traceparent := TraceParent(ctx)
	if traceparent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent = %q", traceparent)
	}

	restored := trace.SpanContextFromContext(ContextWithTraceParent(context.Background(), traceparent))
	if !restored.IsRemote() || restored.TraceID() != testSpanContext(t).TraceID() {
		t.Fatalf("restored span context = %+v", restored)
	}
}

func TestTraceParentWithoutSpanIsEmpty(t *testing.T) {
	if got := TraceParent(context.Background()); got != "" {
		t.Fatalf("traceparent = %q, want empty", got)
	}
}

func TestMalformedTraceParentIsIgnored(t *testing.T) {
	for _, value := range []string{"", "garbage", "00-00000000000000000000000000000000-00f067aa0ba902b7-01"} {
		ctx := ContextWithTraceParent(context.Background(), value)
		if trace.SpanContextFromContext(ctx).IsValid() {
			t.Errorf("value %q produced a valid span context", value)
		}
	}
}

func TestHeaderCarrierRoundTrip(t *testing.T) {
	headers := HeaderCarrier{"unrelated": int64(3)}
	InjectHeaders(trace.ContextWithSpanContext(context.Background(), testSpanContext(t)), headers)

	if headers.Get("unrelated") != "" {
		t.Fatal("non-string header returned as a string")
	}
	restored := trace.SpanContextFromContext(ExtractHeaders(context.Background(), headers))
	if restored.SpanID() != testSpanContext(t).SpanID() {
		t.Fatalf("restored span = %v", restored.SpanID())
	}

	var nilHeaders HeaderCarrier
	if trace.SpanContextFromContext(ExtractHeaders(context.Background(), nilHeaders)).IsValid() {
		t.Fatal("nil headers produced a span context")
	}
}

func TestQueryOperation(t *testing.T) {
	for sql, want := range map[string]string{
		"\n\t\tSELECT id FROM t": "SELECT",
		"with x as (select 1)":   "WITH",
		"begin":                  "BEGIN",
		"VACUUM t":               "QUERY",
		"":                       "QUERY",
	} {
		if got := queryOperation(sql); got != want {
			t.Errorf("queryOperation(%q) = %q, want %q", sql, got, want)
		}
	}
}
