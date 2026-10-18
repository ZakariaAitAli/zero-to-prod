package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/observability"
	"github.com/ZakariaAitAli/zero-to-prod/apps/work-items/internal/runtimeconfig"
	"github.com/prometheus/client_golang/prometheus"
)

// version is replaced at build time with -ldflags="-X main.version=<sha>" and
// reported in logs, spans, and work_items_build_info.
var version = "dev"

const (
	defaultWorkerQueue      = "work_item_processing"
	workerSessionRetryDelay = 1 * time.Second
	workerServiceName       = "work-items-worker"
	telemetryFlushTimeout   = 5 * time.Second
)

type workerConfig struct {
	DatabaseURL       string
	RabbitMQWorkerURL string
	QueueName         string
}

type environmentLookup func(string) string

type workerRuntimeStore interface {
	processingJobCompleter
	Close()
}

type workerRuntimeStoreFactory func(
	context.Context,
	string,
) (
	workerRuntimeStore,
	error,
)

type workerRuntimeSessionFactory func(
	context.Context,
	string,
	string,
) (
	workerDeliverySession,
	error,
)

func loadWorkerConfig(
	lookup environmentLookup,
) (workerConfig, error) {
	databaseURL, err := runtimeconfig.Value(
		lookup,
		"DATABASE_URL",
	)
	if err != nil {
		return workerConfig{}, err
	}

	rabbitMQWorkerURL, err := runtimeconfig.Value(
		lookup,
		"RABBITMQ_WORKER_URL",
	)
	if err != nil {
		return workerConfig{}, err
	}

	queueName := lookup("RABBITMQ_QUEUE")
	if queueName == "" {
		queueName = defaultWorkerQueue
	}

	return workerConfig{
		DatabaseURL:       databaseURL,
		RabbitMQWorkerURL: rabbitMQWorkerURL,
		QueueName:         queueName,
	}, nil
}

func runWorkerProcess(
	ctx context.Context,
	config workerConfig,
	newStore workerRuntimeStoreFactory,
	newSession workerRuntimeSessionFactory,
	reportExit workerSessionExitReporter,
) error {
	store, err := newStore(
		ctx,
		config.DatabaseURL,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize worker PostgreSQL store: %w",
			err,
		)
	}
	defer store.Close()

	sessionFactory := func(
		sessionContext context.Context,
	) (
		workerDeliverySession,
		error,
	) {
		return newSession(
			sessionContext,
			config.RabbitMQWorkerURL,
			config.QueueName,
		)
	}

	runSession := func(
		sessionContext context.Context,
	) error {
		return runWorkerSession(
			sessionContext,
			store,
			sessionFactory,
		)
	}

	return runWorkerLifecycleWithReporter(
		ctx,
		runSession,
		workerSessionRetryDelay,
		reportExit,
	)
}

func main() {
	flushTelemetry, err := observability.Setup(context.Background(), workerServiceName, version, os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
		defer cancel()
		_ = flushTelemetry(ctx)
	}()

	config, err := loadWorkerConfig(
		os.Getenv,
	)
	if err != nil {
		log.Fatalf(
			"worker configuration: %v",
			err,
		)
	}

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// WORKER_METRICS_ADDRESS (for example 0.0.0.0:9465) enables the private
	// Prometheus listener; it is off when unset.
	var registry *prometheus.Registry
	if metricsAddress := os.Getenv("WORKER_METRICS_ADDRESS"); metricsAddress != "" {
		registry = observability.NewRegistry(workerServiceName, version)
		registerWorkerMetrics(registry)

		if err := observability.StartMetricsServer(shutdownContext, metricsAddress, registry); err != nil {
			log.Fatal(err)
		}
	}

	storeFactory := func(
		ctx context.Context,
		databaseURL string,
	) (
		workerRuntimeStore,
		error,
	) {
		store, err := newPostgresWorkerStore(
			ctx,
			databaseURL,
		)
		if err != nil {
			return nil, err
		}
		if registry != nil {
			registry.MustRegister(observability.NewPoolCollector(store.pool))
		}
		return instrumentedWorkerStore{store}, nil
	}

	sessionFactory := func(
		ctx context.Context,
		connectionURL string,
		queueName string,
	) (
		workerDeliverySession,
		error,
	) {
		return newRabbitMQWorkerSession(
			ctx,
			connectionURL,
			queueName,
		)
	}

	// The message texts are matched by the worker experiment harness.
	reportExit := func(err error) {
		workerSessionExits.Inc()

		if err == nil {
			slog.Warn(
				"worker session ended unexpectedly without error; retrying",
			)

			return
		}

		slog.Warn(
			"worker session ended; retrying",
			"error", err,
		)
	}

	slog.Info(
		"worker starting",
		"queue", config.QueueName,
	)

	if err := runWorkerProcess(
		shutdownContext,
		config,
		storeFactory,
		sessionFactory,
		reportExit,
	); err != nil {
		log.Fatalf(
			"worker stopped: %v",
			err,
		)
	}

	slog.Info(
		"worker shutdown complete",
	)
}
