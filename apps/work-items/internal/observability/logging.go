// Package observability holds the logging, tracing, and metrics setup shared
// by the Work Items API and worker (ADR 0012).
package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns a JSON logger that names the service and version and adds
// the active trace_id and span_id to every record logged with a context.
//
// Callers log identifiers (job_id, work_item_id, delivery_id, route patterns),
// never request bodies, query strings, headers, tokens, or connection URLs.
func NewLogger(w io.Writer, service, version string, level slog.Leveler) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(traceHandler{handler}).With(
		slog.String("service", service),
		slog.String("version", version),
	)
}

// newExportingLogger writes each record to w as JSON and to the OpenTelemetry
// log pipeline, which carries the trace context itself and the service
// identity as resource attributes. Both outputs apply the same level.
func newExportingLogger(
	w io.Writer,
	service string,
	version string,
	level slog.Leveler,
	provider otellog.LoggerProvider,
) *slog.Logger {
	stdout := traceHandler{slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})}
	export := levelHandler{
		Handler: otelslog.NewHandler(instrumentationName, otelslog.WithLoggerProvider(provider)),
		level:   level,
	}
	return slog.New(fanoutHandler{stdout, export}).With(
		slog.String("service", service),
		slog.String("version", version),
	)
}

const instrumentationName = "github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/observability"

// fanoutHandler passes each record to every handler that accepts its level.
type fanoutHandler []slog.Handler

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h {
		if handler.Enabled(ctx, record.Level) {
			errs = append(errs, handler.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make(fanoutHandler, len(h))
	for i, handler := range h {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return handlers
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make(fanoutHandler, len(h))
	for i, handler := range h {
		handlers[i] = handler.WithGroup(name)
	}
	return handlers
}

type levelHandler struct {
	slog.Handler
	level slog.Leveler
}

func (h levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level() && h.Handler.Enabled(ctx, level)
}

func (h levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelHandler{h.Handler.WithAttrs(attrs), h.level}
}

func (h levelHandler) WithGroup(name string) slog.Handler {
	return levelHandler{h.Handler.WithGroup(name), h.level}
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, record)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}
