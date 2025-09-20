package consumer

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/hatsunemiku3939/sqsrouter"
)

// FIFOConsumer processes messages sequentially to guarantee order for FIFO queues.
type FIFOConsumer struct {
	client   SQSClient
	queueURL string
	router   *sqsrouter.Router
}

// NewFIFOConsumer creates a new consumer specifically for FIFO SQS queues.
func NewFIFOConsumer(client SQSClient, queueURL string, router *sqsrouter.Router) Consumer {
	return &FIFOConsumer{client: client, queueURL: queueURL, router: router}
}

// Start begins the sequential polling and processing loop.
func (c *FIFOConsumer) Start(ctx context.Context) {
	log.Printf("🚀 SQS FIFO consumer started. Polling queue: %s. Press Ctrl+C to shut down.", c.queueURL)

	for {
		// Before polling, check if a shutdown has been initiated.
		if ctx.Err() != nil {
			log.Println("INFO: Shutdown initiated, no longer polling for new messages.")
			break
		}

		output, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: maxMessages,
			WaitTimeSeconds:     waitTimeSeconds,
			MessageSystemAttributeNames: []sqstypes.MessageSystemAttributeName{
				sqstypes.MessageSystemAttributeNameAll,
			},
			MessageAttributeNames: []string{"All"},
		})

		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Println("INFO: Context canceled by shutdown signal. Stopping poller.")
				break // Exit the loop cleanly.
			}
			log.Printf("ERROR: Failed to receive messages: %v. Retrying...", err)
			// Wait before retrying on other errors.
			// Sequential consumer maintains same backoff as standard consumer.
			time.Sleep(retrySleep)
			continue
		}

		if len(output.Messages) == 0 {
			continue
		}

		log.Printf("INFO: Received %d messages.", len(output.Messages))

		// Process messages sequentially
		stopBatch := false
		for _, m := range output.Messages {
			if stopBatch {
				break
			}
			// Per-message timeout
			msgCtx, cancelMsg := context.WithTimeout(context.Background(), processingTimeout) //nolint:contextcheck
			// Attach MessageContext built from SQS attributes
			msgCtx = sqsrouter.WithMessageContext(msgCtx, buildMessageContext(&m))
			stopBatch = !c.processMessage(msgCtx, &m) //nolint:contextcheck
			cancelMsg()
		}
	}

	log.Println("✅ Graceful shutdown complete (FIFO consumer).")
}

// processMessage handles a single message and returns true if batch can continue, false to stop batch.
func (c *FIFOConsumer) processMessage(ctx context.Context, msg *sqstypes.Message) bool {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("ERROR: Panic recovered while processing a message: %v", rec)
		}
	}()

	if msg.Body == nil {
		log.Println("ERROR: Received message with empty body.")
		// Treat as failure; do not continue the batch to preserve order.
		return false
	}

	routed := c.router.Route(ctx, []byte(*msg.Body))

	if routed.HandlerResult.Error != nil {
		log.Printf("❌ FAILURE [%s] %s v%s (%s): %v",
			routed.Timestamp,
			routed.MessageType,
			routed.MessageVersion,
			routed.MessageID,
			routed.HandlerResult.Error,
		)
	} else {
		log.Printf("✅ SUCCESS [%s] %s v%s (%s)",
			routed.Timestamp,
			routed.MessageType,
			routed.MessageVersion,
			routed.MessageID,
		)
	}

	if routed.HandlerResult.ShouldDelete {
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancelDelete()

		//nolint:contextcheck
		_, err := c.client.DeleteMessage(deleteCtx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(c.queueURL),
			ReceiptHandle: msg.ReceiptHandle,
		})

		if err != nil {
			log.Printf("ERROR: Failed to delete message ID %s: %v", routed.MessageID, err)
			// Deletion failure can break ordering; stop batch to avoid processing newer messages first.
			return false
		}

		log.Printf("🗑️  Deleted message ID %s", routed.MessageID)
		return true
	}

	log.Printf("🔁 RETRYING message ID %s later (visibility timeout will expire).", routed.MessageID)
	// Fail-fast: stop processing remaining messages in the batch.
	return false
}

// no extra helpers needed; FIFO uses same timing constants as standard consumer.
