package sqsrouter

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// --- SQS Consumer Configuration ---
const (
	// maxMessages defines the maximum number of messages to retrieve in one SQS API call.
	maxMessages = 5
	// waitTimeSeconds enables SQS Long Polling, reducing cost and empty responses.
	waitTimeSeconds = 10
	// deleteTimeout sets a client-side timeout for the DeleteMessage API call.
	deleteTimeout = 5 * time.Second
	// processingTimeout sets a deadline for processing a single message.
	// This should be less than the container's graceful shutdown period (e.g., terminationGracePeriodSeconds in K8s).
	processingTimeout = 30 * time.Second
)

// SQSClient defines the interface for SQS operations needed by the Consumer.
// This allows for easier testing by mocking the SQS client.
type SQSClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

// Consumer encapsulates the SQS polling and message processing logic.
type Consumer struct {
	client   SQSClient
	queueURL string
	router   *Router
}

// NewConsumer creates a new SQS message consumer.
func NewConsumer(client SQSClient, queueURL string, router *Router) *Consumer {
	return &Consumer{
		client:   client,
		queueURL: queueURL,
		router:   router,
	}
}

// Start begins the consumer's polling loop. It blocks until the context is canceled.
func (c *Consumer) Start(ctx context.Context) {
	log.Printf("🚀 SQS consumer started. Polling queue: %s. Press Ctrl+C to shut down.", c.queueURL)
	var wg sync.WaitGroup

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
		})

		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Println("INFO: Context canceled by shutdown signal. Stopping poller.")
				break // Exit the loop cleanly.
			}
			log.Printf("ERROR: Failed to receive messages: %v. Retrying...", err)
			time.Sleep(2 * time.Second) // Wait before retrying on other errors.
			continue
		}

		if len(output.Messages) == 0 {
			continue
		}

		log.Printf("INFO: Received %d messages.", len(output.Messages))

		for _, msg := range output.Messages {
			wg.Add(1)
			go func(m types.Message) {
				defer wg.Done()
				msgCtx, cancelMsg := context.WithTimeout(context.Background(), processingTimeout)
				defer cancelMsg()
				c.processMessage(msgCtx, &m)
			}(msg)
		}
	}

	log.Println("INFO: Waiting for in-flight messages to be processed...")
	wg.Wait()
	log.Println("✅ Graceful shutdown complete. All processed messages are handled.")
}

// processMessage routes, handles, and deletes a single SQS message.
func (c *Consumer) processMessage(ctx context.Context, msg *types.Message) {
	if msg.Body == nil {
		log.Println("ERROR: Received message with empty body.")
		return
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

		_, err := c.client.DeleteMessage(deleteCtx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(c.queueURL),
			ReceiptHandle: msg.ReceiptHandle,
		})

		if err != nil {
			log.Printf("ERROR: Failed to delete message ID %s: %v", routed.MessageID, err)
		} else {
			log.Printf("🗑️  Deleted message ID %s", routed.MessageID)
		}
	} else {
		log.Printf("🔁 RETRYING message ID %s later (visibility timeout will expire).", routed.MessageID)
	}
}
