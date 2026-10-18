package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// traceContext is the only propagator used: W3C traceparent and tracestate,
// without baggage, so no application data crosses a process boundary.
var traceContext = propagation.TraceContext{}

// Setup installs the global tracer provider, propagator, and default logger.
//
// Telemetry is exported over OTLP/HTTP only when OTEL_EXPORTER_OTLP_ENDPOINT
// (or the signal-specific OTEL_EXPORTER_OTLP_TRACES_ENDPOINT /
// OTEL_EXPORTER_OTLP_LOGS_ENDPOINT) is set; the exporters read the standard
// OTEL_EXPORTER_OTLP_* variables, and OTEL_LOGS_EXPORTER=none keeps logs on
// stdout only. Without an endpoint, spans are still created so logs carry
// trace IDs, and nothing leaves the process. Sampling follows
// OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG (default
// parentbased_always_on). Logs always go to stdout as JSON; LOG_LEVEL accepts
// debug, info, warn, or error (default info).
//
// The returned function flushes buffered telemetry; call it during shutdown.
func Setup(
	ctx context.Context,
	service string,
	version string,
	getenv func(string) string,
) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(traceContext)

	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", service),
			attribute.String("service.version", version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}

	traceOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if exportEnabled(getenv, "TRACES") {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
		}
		traceOptions = append(traceOptions, sdktrace.WithBatcher(exporter))
	}
	tracerProvider := sdktrace.NewTracerProvider(traceOptions...)
	otel.SetTracerProvider(tracerProvider)
	shutdowns := []func(context.Context) error{tracerProvider.Shutdown}

	level := parseLevel(getenv("LOG_LEVEL"))
	logger := NewLogger(os.Stdout, service, version, level)
	if exportEnabled(getenv, "LOGS") && getenv("OTEL_LOGS_EXPORTER") != "none" {
		exporter, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("create OTLP log exporter: %w", err)
		}
		loggerProvider := sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		)
		logger = newExportingLogger(os.Stdout, service, version, level, loggerProvider)
		shutdowns = append(shutdowns, loggerProvider.Shutdown)
	}
	// Also routes the standard log package (log.Printf) through this logger.
	slog.SetDefault(logger)

	return func(ctx context.Context) error {
		var errs []error
		for _, shutdown := range shutdowns {
			errs = append(errs, shutdown(ctx))
		}
		return errors.Join(errs...)
	}, nil
}

func exportEnabled(getenv func(string) string, signal string) bool {
	return getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		getenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT") != ""
}

// TraceParent returns the W3C traceparent of the span in ctx, or "" when ctx
// has no valid span context. Only traceparent is returned (no tracestate), so
// the persisted value has a fixed format and length.
func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	traceContext.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// ContextWithTraceParent returns ctx carrying the remote span context named by
// traceparent. An empty or malformed value leaves ctx unchanged, so a missing
// trace context never blocks delivery.
func ContextWithTraceParent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return traceContext.Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

// HeaderCarrier adapts message headers (such as amqp.Table, which has the same
// underlying type) for trace-context injection and extraction. Non-string
// values are ignored.
type HeaderCarrier map[string]any

func (c HeaderCarrier) Get(key string) string {
	value, _ := c[key].(string)
	return value
}

func (c HeaderCarrier) Set(key, value string) {
	c[key] = value
}

func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

// InjectHeaders writes the trace context of ctx into headers.
func InjectHeaders(ctx context.Context, headers HeaderCarrier) {
	traceContext.Inject(ctx, headers)
}

// ExtractHeaders returns ctx carrying the remote span context from headers.
func ExtractHeaders(ctx context.Context, headers HeaderCarrier) context.Context {
	return traceContext.Extract(ctx, headers)
}
