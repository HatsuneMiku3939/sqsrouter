package consumer

import "context"

// Consumer defines the standard interface for an SQS message consumer.
// It is responsible for starting the message polling and processing loop.
type Consumer interface {
	// Start begins the consumer's polling loop. It blocks until the context is canceled.
	Start(ctx context.Context)
}
