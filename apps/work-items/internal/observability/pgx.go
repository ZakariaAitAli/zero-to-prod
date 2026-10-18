package observability

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const queryTextLimit = 512

// QueryTracer records a client span for each PostgreSQL query issued under an
// existing span. Queries without a parent span (the outbox poll, for example)
// are not traced, so idle loops create no traces.
//
// Spans carry the static SQL text, never bound arguments, and only the
// SQLSTATE of a failure: PostgreSQL error messages can quote row values.
type QueryTracer struct{}

var _ pgx.QueryTracer = QueryTracer{}

func (QueryTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}

	operation := queryOperation(data.SQL)
	text := strings.TrimSpace(data.SQL)
	if len(text) > queryTextLimit {
		text = text[:queryTextLimit]
	}

	ctx, _ = otel.Tracer("github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/observability").Start(
		ctx,
		operation+" postgresql",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", operation),
			attribute.String("db.query.text", text),
		),
	)
	return ctx
}

func (QueryTracer) TraceQueryEnd(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryEndData,
) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	defer span.End()

	if data.Err == nil {
		return
	}

	description := "query failed"
	var pgErr *pgconn.PgError
	if errors.As(data.Err, &pgErr) {
		span.SetAttributes(attribute.String("db.response.status_code", pgErr.Code))
		description = "SQLSTATE " + pgErr.Code
	} else if errors.Is(data.Err, context.DeadlineExceeded) {
		description = "deadline exceeded"
	} else if errors.Is(data.Err, context.Canceled) {
		description = "canceled"
	}
	span.SetStatus(codes.Error, description)
}

func queryOperation(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "QUERY"
	}
	operation := strings.ToUpper(fields[0])
	switch operation {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "BEGIN", "COMMIT", "ROLLBACK", "SET", "LOCK":
		return operation
	default:
		return "QUERY"
	}
}
