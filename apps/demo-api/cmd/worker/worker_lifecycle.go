package main

import (
	"context"
	"time"
)

type workerSessionRunner func(
	context.Context,
) error

type workerSessionExitReporter func(error)

func waitForWorkerRetryDelay(
	ctx context.Context,
	delay time.Duration,
) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false

		default:
			return true
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false

	case <-timer.C:
		return true
	}
}

func runWorkerLifecycle(
	ctx context.Context,
	runSession workerSessionRunner,
	retryDelay time.Duration,
) error {
	return runWorkerLifecycleWithReporter(
		ctx,
		runSession,
		retryDelay,
		nil,
	)
}

func runWorkerLifecycleWithReporter(
	ctx context.Context,
	runSession workerSessionRunner,
	retryDelay time.Duration,
	reportExit workerSessionExitReporter,
) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		sessionErr := runSession(ctx)

		// A session returning because shutdown was requested is normal.
		// Do not report it as a recoverable failure/restart condition.
		if ctx.Err() != nil {
			return nil
		}

		if reportExit != nil {
			reportExit(sessionErr)
		}

		if !waitForWorkerRetryDelay(
			ctx,
			retryDelay,
		) {
			return nil
		}
	}
}
