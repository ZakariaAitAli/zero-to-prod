package main

import "context"

type processingJobProcessor interface {
	Process(
		context.Context,
		workerMessage,
	) error
}

type processingJobProcessorFunc func(
	context.Context,
	workerMessage,
) error

func (processor processingJobProcessorFunc) Process(
	ctx context.Context,
	message workerMessage,
) error {
	return processor(ctx, message)
}

func successfulProcessingJobProcessor(
	context.Context,
	workerMessage,
) error {
	return nil
}
