package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry returns a registry with Go runtime, process, and build metrics.
// A private registry (not the global default) keeps the exposed series to
// those registered on purpose.
func NewRegistry(service, version string) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "work_items_build_info",
			Help:        "Always 1; labels name the running service and version.",
			ConstLabels: prometheus.Labels{"service": service, "version": version},
		}, func() float64 { return 1 }),
	)
	return registry
}

// StartMetricsServer listens on address before returning, so a bad address
// fails startup, then serves /metrics until ctx ends. The listener is
// separate from the application port: the ingress never routes to it.
func StartMetricsServer(ctx context.Context, address string, gatherer prometheus.Gatherer) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen metrics: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		ErrorHandling:       promhttp.ContinueOnError,
		MaxRequestsInFlight: 4,
		Timeout:             5 * time.Second,
	}))
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	slog.Info("metrics listening", "address", listener.Addr().String())
	return nil
}

// poolCollector exposes pgxpool statistics, read at scrape time.
type poolCollector struct {
	pool *pgxpool.Pool

	acquired, idle, constructing, total, max         *prometheus.Desc
	acquires, emptyAcquires, canceledAcquires        *prometheus.Desc
	acquireSeconds, emptyAcquireWaitSeconds          *prometheus.Desc
	newConnections, maxLifetimeClosed, maxIdleClosed *prometheus.Desc
}

// NewPoolCollector returns a collector for the connection pool. Pool waits
// (empty acquires and their wait time) are the saturation signal for the
// database client side.
func NewPoolCollector(pool *pgxpool.Pool) prometheus.Collector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("work_items_db_pool_"+name, help, nil, nil)
	}
	return &poolCollector{
		pool:                    pool,
		acquired:                desc("acquired_connections", "Connections currently in use."),
		idle:                    desc("idle_connections", "Idle connections in the pool."),
		constructing:            desc("constructing_connections", "Connections being established."),
		total:                   desc("total_connections", "All connections in the pool."),
		max:                     desc("max_connections", "Configured maximum pool size."),
		acquires:                desc("acquires_total", "Successful connection acquisitions."),
		emptyAcquires:           desc("empty_acquires_total", "Acquisitions that had to wait because the pool was empty."),
		canceledAcquires:        desc("canceled_acquires_total", "Acquisitions canceled by their context."),
		acquireSeconds:          desc("acquire_seconds_total", "Total time spent acquiring connections."),
		emptyAcquireWaitSeconds: desc("empty_acquire_wait_seconds_total", "Total time spent waiting for a connection from an empty pool."),
		newConnections:          desc("new_connections_total", "Connections opened."),
		maxLifetimeClosed:       desc("max_lifetime_closed_total", "Connections closed for reaching their maximum lifetime."),
		maxIdleClosed:           desc("max_idle_closed_total", "Connections closed for idling too long."),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.acquired, c.idle, c.constructing, c.total, c.max,
		c.acquires, c.emptyAcquires, c.canceledAcquires,
		c.acquireSeconds, c.emptyAcquireWaitSeconds,
		c.newConnections, c.maxLifetimeClosed, c.maxIdleClosed,
	} {
		ch <- desc
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	gauge := func(desc *prometheus.Desc, value float64) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value)
	}
	counter := func(desc *prometheus.Desc, value float64) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value)
	}
	gauge(c.acquired, float64(stat.AcquiredConns()))
	gauge(c.idle, float64(stat.IdleConns()))
	gauge(c.constructing, float64(stat.ConstructingConns()))
	gauge(c.total, float64(stat.TotalConns()))
	gauge(c.max, float64(stat.MaxConns()))
	counter(c.acquires, float64(stat.AcquireCount()))
	counter(c.emptyAcquires, float64(stat.EmptyAcquireCount()))
	counter(c.canceledAcquires, float64(stat.CanceledAcquireCount()))
	counter(c.acquireSeconds, stat.AcquireDuration().Seconds())
	counter(c.emptyAcquireWaitSeconds, stat.EmptyAcquireWaitTime().Seconds())
	counter(c.newConnections, float64(stat.NewConnsCount()))
	counter(c.maxLifetimeClosed, float64(stat.MaxLifetimeDestroyCount()))
	counter(c.maxIdleClosed, float64(stat.MaxIdleDestroyCount()))
}
