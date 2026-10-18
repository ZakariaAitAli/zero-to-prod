package main

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Metric labels are bounded (ADR 0012): route is the registered pattern or
// "unmatched", method is GET, POST, or "other", and code is the response
// status. Identifiers such as job IDs go to logs and spans, never labels.

var apiTracer = otel.Tracer("github.com/ZakariaAitAli/zero-to-prod/apps/work-items/api")

var (
	// 0.25 s is the latency SLO threshold; keep it a bucket boundary.
	httpDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "work_items_http_requests_total",
		Help: "HTTP requests by route pattern, method, and status code.",
	}, []string{"route", "method", "code"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "work_items_http_request_duration_seconds",
		Help:    "HTTP request duration by route pattern and method.",
		Buckets: httpDurationBuckets,
	}, []string{"route", "method"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "work_items_http_requests_in_flight",
		Help: "HTTP requests being served.",
	})

	outboxPublishResults = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "work_items_outbox_publish_total",
		Help: "Outbox publish attempts by result: published, broker_error, or store_error.",
	}, []string{"result"})

	outboxPublishDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "work_items_outbox_publish_duration_seconds",
		Help:    "Time from publish to broker confirmation or failure.",
		Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	})

	outboxPublishLag = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "work_items_outbox_publish_lag_seconds",
		Help:    "Time from acceptance (outbox row creation) to confirmed publication.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	})

	outboxBrokerConnectFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "work_items_outbox_broker_connect_failures_total",
		Help: "Failed attempts to open the outbox publisher's broker connection.",
	})
)

func registerAPIMetrics(registry prometheus.Registerer, store *postgresStore) {
	registry.MustRegister(
		httpRequests,
		httpDuration,
		httpInFlight,
		outboxPublishResults,
		outboxPublishDuration,
		outboxPublishLag,
		outboxBrokerConnectFailures,
		newBacklogCollector(store),
	)
}

// instrumentHTTP records a server span, metrics, and one log record per
// request. Inbound traceparent headers are ignored: requests arrive from the
// internet through the ingress, and an external caller should not choose
// trace IDs or sampling. Each request starts a new trace.
func instrumentHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		httpInFlight.Inc()
		defer httpInFlight.Dec()

		ctx, span := apiTracer.Start(
			r.Context(),
			r.Method,
			trace.WithNewRoot(),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		r = r.WithContext(ctx)
		next.ServeHTTP(recorder, r)

		// The mux sets Pattern on the request it routes.
		route := routeLabel(r.Pattern)
		method := methodLabel(r.Method)
		code := strconv.Itoa(recorder.status)
		elapsed := time.Since(started)

		httpRequests.WithLabelValues(route, method, code).Inc()
		httpDuration.WithLabelValues(route, method).Observe(elapsed.Seconds())

		span.SetName(method + " " + route)
		span.SetAttributes(
			attribute.String("http.request.method", method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", recorder.status),
		)
		if recorder.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, code)
		}

		level := slog.LevelInfo
		switch {
		case operationalRoute(route):
			level = slog.LevelDebug
		case recorder.status >= http.StatusInternalServerError:
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "http request",
			"method", method,
			"route", route,
			"status", recorder.status,
			"duration_ms", float64(elapsed.Microseconds())/1000,
		)
	})
}

func routeLabel(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if _, path, found := strings.Cut(pattern, " "); found {
		return path
	}
	return pattern
}

func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost:
		return method
	default:
		return "other"
	}
}

func operationalRoute(route string) bool {
	switch route {
	case "/health", "/ready", "/version":
		return true
	default:
		return false
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(body)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// backlogCollector reads the durable async backlog at scrape time. These are
// the symptoms an async user sees: accepted work that is not yet published or
// not yet finished, and how long the oldest has waited. When PostgreSQL is
// unreachable only work_items_backlog_scrape_success (0) is reported, so a
// missing series is not mistaken for an empty backlog.
type backlogCollector struct {
	store *postgresStore

	success, unpublished, oldestUnpublished, accepted, oldestAccepted *prometheus.Desc
}

const backlogQueryTimeout = time.Second

func newBacklogCollector(store *postgresStore) *backlogCollector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("work_items_"+name, help, nil, nil)
	}
	return &backlogCollector{
		store:             store,
		success:           desc("backlog_scrape_success", "1 if the backlog query succeeded during this scrape."),
		unpublished:       desc("outbox_unpublished_messages", "Outbox messages not yet confirmed by the broker."),
		oldestUnpublished: desc("outbox_oldest_unpublished_age_seconds", "Age of the oldest unpublished outbox message (0 if none)."),
		accepted:          desc("processing_jobs_accepted", "Processing jobs accepted but not finished."),
		oldestAccepted:    desc("processing_job_oldest_accepted_age_seconds", "Age of the oldest accepted, unfinished processing job (0 if none)."),
	}
}

func (c *backlogCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.success
	ch <- c.unpublished
	ch <- c.oldestUnpublished
	ch <- c.accepted
	ch <- c.oldestAccepted
}

func (c *backlogCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), backlogQueryTimeout)
	defer cancel()

	snapshot, err := c.store.backlog(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.success, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.success, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.unpublished, prometheus.GaugeValue, float64(snapshot.unpublished))
	ch <- prometheus.MustNewConstMetric(c.oldestUnpublished, prometheus.GaugeValue, snapshot.oldestUnpublishedSeconds)
	ch <- prometheus.MustNewConstMetric(c.accepted, prometheus.GaugeValue, float64(snapshot.accepted))
	ch <- prometheus.MustNewConstMetric(c.oldestAccepted, prometheus.GaugeValue, snapshot.oldestAcceptedSeconds)
}

type backlogSnapshot struct {
	unpublished              int64
	oldestUnpublishedSeconds float64
	accepted                 int64
	oldestAcceptedSeconds    float64
}

// backlog uses the partial index on unpublished outbox rows; accepted jobs
// are few by construction (at most one per Work Item).
func (store *postgresStore) backlog(ctx context.Context) (backlogSnapshot, error) {
	const query = `
		SELECT
			(SELECT count(*) FROM public.outbox_messages WHERE published_at IS NULL),
			COALESCE((SELECT EXTRACT(EPOCH FROM now() - min(created_at))::float8
				FROM public.outbox_messages WHERE published_at IS NULL), 0),
			(SELECT count(*) FROM public.processing_jobs WHERE state = 'accepted'),
			COALESCE((SELECT EXTRACT(EPOCH FROM now() - min(created_at))::float8
				FROM public.processing_jobs WHERE state = 'accepted'), 0)
	`
	var snapshot backlogSnapshot
	err := store.pool.QueryRow(ctx, query).Scan(
		&snapshot.unpublished,
		&snapshot.oldestUnpublishedSeconds,
		&snapshot.accepted,
		&snapshot.oldestAcceptedSeconds,
	)
	return snapshot, err
}
