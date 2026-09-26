package main

import (
	"context"
	"errors"
	"fmt"
)

type applicationService func(context.Context) error

type applicationServiceResult struct {
	name string
	err  error
}

func runApplication(
	parentContext context.Context,
	runHTTP applicationService,
	runPublisher applicationService,
) error {
	applicationContext, cancel := context.WithCancel(parentContext)
	defer cancel()

	results := make(chan applicationServiceResult, 2)

	startService := func(
		name string,
		service applicationService,
	) {
		go func() {
			results <- applicationServiceResult{
				name: name,
				err:  service(applicationContext),
			}
		}()
	}

	startService("HTTP server", runHTTP)
	startService("outbox publisher", runPublisher)

	first := <-results

	var firstErr error

	switch {
	case first.err != nil:
		if !(parentContext.Err() != nil &&
			errors.Is(first.err, context.Canceled)) {
			firstErr = fmt.Errorf(
				"%s: %w",
				first.name,
				first.err,
			)
		}

	case parentContext.Err() == nil:
		firstErr = fmt.Errorf(
			"%s stopped unexpectedly",
			first.name,
		)
	}

	// Whether shutdown was requested externally or one service exited,
	// stop the other service before returning to main. This guarantees
	// resources shared by both services remain valid until both are done.
	cancel()

	second := <-results

	var secondErr error

	if second.err != nil &&
		!errors.Is(second.err, context.Canceled) {
		secondErr = fmt.Errorf(
			"%s: %w",
			second.name,
			second.err,
		)
	}

	return errors.Join(
		firstErr,
		secondErr,
	)
}
