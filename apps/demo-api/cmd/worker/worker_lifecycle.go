package main

import (
	"context"
	"time"
)

type workerSessionRunner func(
	context.Context,
) error

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
	for {
		if ctx.Err() != nil {
			return nil
		}

		_ = runSession(ctx)

		if ctx.Err() != nil {
			return nil
		}

		if !waitForWorkerRetryDelay(
			ctx,
			retryDelay,
		) {
			return nil
		}
	}
}
