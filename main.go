package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/xeipuuv/gojsonschema"
)

// --- Constants for Message Types and Versions ---
// Using constants for message types and versions prevents typos and improves maintainability.
const (
	MsgTypeUpdateUserProfile = "updateUserProfile"
	MsgVersion1_0            = "1.0"
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

// --- Message Payloads & Metadata ---

// UserProfileMessage defines the structure for the "updateUserProfile" message payload.
type UserProfileMessage struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// messageEnvelope is an internal struct to unmarshal the outer layer of an SQS message.
// It contains the routing information and the actual message payload.
type messageEnvelope struct {
	SchemaVersion  string          `json:"schemaVersion"`
	MessageType    string          `json:"messageType"`
	MessageVersion string          `json:"messageVersion"`
	Message        json.RawMessage `json:"message"`
	Metadata       json.RawMessage `json:"metadata"`
}

// messageMetadata holds common metadata found in every message.
type messageMetadata struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	MessageID string `json:"messageId"`
}

// --- Handler & Router Logic ---

// HandlerResult indicates the outcome of processing a message.
type HandlerResult struct {
	// ShouldDelete is true if the message was processed (successfully or not) and should be deleted from the queue.
	// Set to false for transient errors where a retry is desired.
	ShouldDelete bool
	// Error contains any error that occurred during processing. nil for success.
	Error error
}

// RoutedResult contains the complete result after a message has been routed and handled.
type RoutedResult struct {
	MessageType    string
	MessageVersion string
	HandlerResult  HandlerResult
	MessageID      string
	Timestamp      string
}

// MessageHandler is a function type that processes a specific message type and version.
// It receives the message payload and metadata as raw JSON bytes.
type MessageHandler func(ctx context.Context, messageJSON []byte, metadataJSON []byte) HandlerResult

// Router routes incoming messages to the correct handler based on message type and version.
// It is safe for concurrent use.
type Router struct {
	mu             sync.RWMutex
	handlers       map[string]MessageHandler
	schemas        map[string]gojsonschema.JSONLoader
	envelopeSchema gojsonschema.JSONLoader
}

// NewRouter creates and initializes a new Router with a given envelope schema.
func NewRouter(envelopeSchema string) (*Router, error) {
	loader := gojsonschema.NewStringLoader(envelopeSchema)
	// Validate the schema itself upon creation to fail fast.
	if _, err := gojsonschema.NewSchema(loader); err != nil {
		return nil, fmt.Errorf("invalid envelope schema: %w", err)
	}

	return &Router{
		handlers:       make(map[string]MessageHandler),
		schemas:        make(map[string]gojsonschema.JSONLoader),
		envelopeSchema: loader,
	}, nil
}

// makeKey creates a consistent key for maps from message type and version.
func makeKey(messageType, messageVersion string) string {
	return fmt.Sprintf("%s:%s", messageType, messageVersion)
}

// Register adds a new message handler for a specific message type and version.
func (r *Router) Register(messageType, messageVersion string, handler MessageHandler) {
	key := makeKey(messageType, messageVersion)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[key] = handler
}

// RegisterSchema adds a JSON schema for validating a specific message type and version.
func (r *Router) RegisterSchema(messageType, messageVersion string, schema string) error {
	loader := gojsonschema.NewStringLoader(schema)
	// Validate the schema itself upon registration.
	if _, err := gojsonschema.NewSchema(loader); err != nil {
		return fmt.Errorf("invalid schema for %s:%s: %w", messageType, messageVersion, err)
	}

	key := makeKey(messageType, messageVersion)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemas[key] = loader
	return nil
}

// formatSchemaError is a helper to create a user-friendly error from gojsonschema validation results.
func formatSchemaError(result *gojsonschema.Result, err error) error {
	if err != nil {
		return fmt.Errorf("schema validation system error: %w", err)
	}
	if result.Valid() {
		return nil
	}

	var errMsg string
	for _, desc := range result.Errors() {
		errMsg += fmt.Sprintf("- %s; ", desc)
	}
	return fmt.Errorf("schema validation failed: %s", errMsg)
}

// Route validates and dispatches a raw message to the appropriate registered handler.
func (r *Router) Route(ctx context.Context, rawMessage []byte) RoutedResult {
	// 1. Validate the message against the envelope schema.
	// This ensures the message has the basic structure required for routing.
	result, err := gojsonschema.Validate(r.envelopeSchema, gojsonschema.NewBytesLoader(rawMessage))
	if validationErr := formatSchemaError(result, err); validationErr != nil {
		return RoutedResult{
			MessageType:    "unknown",
			MessageVersion: "unknown",
			HandlerResult: HandlerResult{
				ShouldDelete: true, // Malformed envelope is a permanent failure.
				Error:        fmt.Errorf("invalid envelope: %w", validationErr),
			},
		}
	}

	// 2. Unmarshal the envelope to access routing info and payload.
	var envelope messageEnvelope
	if err := json.Unmarshal(rawMessage, &envelope); err != nil {
		return RoutedResult{
			MessageType:    "unknown",
			MessageVersion: "unknown",
			HandlerResult: HandlerResult{
				ShouldDelete: true, // JSON parsing error is a permanent failure.
				Error:        fmt.Errorf("failed to parse envelope: %w", err),
			},
		}
	}

	key := makeKey(envelope.MessageType, envelope.MessageVersion)

	// 3. Find the handler and schema for the message.
	r.mu.RLock()
	handler, handlerExists := r.handlers[key]
	schemaLoader, schemaExists := r.schemas[key]
	r.mu.RUnlock()

	if !handlerExists {
		return RoutedResult{
			MessageType:    envelope.MessageType,
			MessageVersion: envelope.MessageVersion,
			HandlerResult: HandlerResult{
				ShouldDelete: true, // No handler means we can't process it, ever.
				Error:        fmt.Errorf("no handler registered for %s", key),
			},
		}
	}

	// 4. If a schema is registered for this message type, validate the payload.
	if schemaExists {
		result, err := gojsonschema.Validate(schemaLoader, gojsonschema.NewBytesLoader(envelope.Message))
		if validationErr := formatSchemaError(result, err); validationErr != nil {
			return RoutedResult{
				MessageType:    envelope.MessageType,
				MessageVersion: envelope.MessageVersion,
				HandlerResult: HandlerResult{
					ShouldDelete: true, // Invalid payload is a permanent failure.
					Error:        fmt.Errorf("invalid message payload: %w", validationErr),
				},
			}
		}
	}

	// 5. Parse metadata for logging/tracing.
	var meta messageMetadata
	if err := json.Unmarshal(envelope.Metadata, &meta); err != nil {
		// Log as a warning because the message can still be processed,
		// but context for logging might be missing.
		log.Printf("⚠️  Warning: could not parse metadata for message. Error: %v", err)
	}

	// 6. Execute the handler with the validated message payload.
	handlerResult := handler(ctx, envelope.Message, envelope.Metadata)

	return RoutedResult{
		MessageType:    envelope.MessageType,
		MessageVersion: envelope.MessageVersion,
		HandlerResult:  handlerResult,
		MessageID:      meta.MessageID,
		Timestamp:      meta.Timestamp,
	}
}

// --- Schemas ---
// Schemas are defined as variables for clarity and separation.

var envelopeSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "schemaVersion": { "type": "string" },
    "messageType": { "type": "string" },
    "messageVersion": { "type": "string" },
    "message": { "type": "object" },
    "metadata": { "type": "object" }
  },
  "required": ["schemaVersion", "messageType", "messageVersion", "message", "metadata"]
}`

var userProfileSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "userId": { "type": "string" },
    "username": { "type": "string" },
    "email": { "type": "string", "format": "email" }
  },
  "required": ["userId", "username", "email"]
}`

// --- Message Handlers ---

// UpdateUserProfileV1Handler handles the logic for updating a user profile.
func UpdateUserProfileV1Handler(ctx context.Context, messageJSON []byte, metadataJSON []byte) HandlerResult {
	var msg UserProfileMessage
	if err := json.Unmarshal(messageJSON, &msg); err != nil {
		// This error should theoretically not happen if schema validation is correct.
		// But as a safeguard, we handle it as a permanent failure.
		return HandlerResult{ShouldDelete: true, Error: fmt.Errorf("failed to unmarshal user profile message: %w", err)}
	}

	// In a real application, this is where you would interact with a database or another service.
	log.Printf("⚙️  Processing user update for %s (ID: %s)", msg.Username, msg.UserID)

	// Simulate work that can be canceled.
	select {
	case <-time.After(2 * time.Second): // Simulate 2 seconds of work.
		log.Printf("INFO: Finished processing for user %s", msg.UserID)
		// For this example, we assume the operation always succeeds.
		return HandlerResult{ShouldDelete: true, Error: nil}
	case <-ctx.Done(): // This case will be hit if the processingTimeout is exceeded.
		log.Printf("WARN: Processing canceled for user %s: %v", msg.UserID, ctx.Err())
		// Return ShouldDelete: false to allow for a retry.
		return HandlerResult{ShouldDelete: false, Error: ctx.Err()}
	}
}

// --- SQS Consumer Entry Point ---

func main() {
	// --- 1. Setup Context for Graceful Shutdown ---
	// Create a context that will be canceled when a shutdown signal is received.
	appCtx, cancelApp := context.WithCancel(context.Background())

	// Create a channel to listen for OS signals.
	shutdownChan := make(chan os.Signal, 1)
	// Notify the channel for SIGINT (Ctrl+C) and SIGTERM (used by orchestrators).
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	// Start a goroutine to wait for a signal.
	go func() {
		sig := <-shutdownChan
		log.Printf("🛑 Received shutdown signal: %v. Starting graceful shutdown...", sig)
		// Cancel the application context to signal the polling loop to stop.
		cancelApp()
	}()

	// --- 2. Load Configuration ---
	// Load AWS config from environment (credentials, region, etc.)
	cfg, err := config.LoadDefaultConfig(appCtx)
	if err != nil {
		log.Fatalf("FATAL: Failed to load AWS config: %v", err)
	}

	// Get Queue URL from environment variable for flexibility.
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("FATAL: SQS_QUEUE_URL environment variable is not set.")
	}

	sqsClient := sqs.NewFromConfig(cfg)

	// --- 3. Setup Router and Register Handlers ---
	router, err := NewRouter(envelopeSchema)
	if err != nil {
		log.Fatalf("FATAL: Could not initialize router: %v", err)
	}

	router.Register(MsgTypeUpdateUserProfile, MsgVersion1_0, UpdateUserProfileV1Handler)
	if err := router.RegisterSchema(MsgTypeUpdateUserProfile, MsgVersion1_0, userProfileSchema); err != nil {
		log.Fatalf("FATAL: Could not register schema: %v", err)
	}

	log.Printf("🚀 SQS consumer started. Polling queue: %s. Press Ctrl+C to shut down.", queueURL)

	// --- 4. Start the Main Consumer Loop ---
	// This WaitGroup tracks all active workers to ensure they finish before the app exits.
	var wg sync.WaitGroup
	for {
		// Before polling, check if a shutdown has been initiated.
		if appCtx.Err() != nil {
			log.Println("INFO: Shutdown initiated, no longer polling for new messages.")
			break
		}

		output, err := sqsClient.ReceiveMessage(appCtx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: maxMessages,
			WaitTimeSeconds:     waitTimeSeconds,
		})

		// If the context was canceled, ReceiveMessage returns an error.
		// We check for this specific error to perform a clean exit from the polling loop.
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
			// This is normal with long polling if no messages arrive.
			continue
		}

		log.Printf("INFO: Received %d messages.", len(output.Messages))

		for _, msg := range output.Messages {
			// Add a worker to the WaitGroup before starting the goroutine.
			wg.Add(1)
			go func(m types.Message) {
				// Ensure the WaitGroup counter is decremented when the goroutine finishes.
				defer wg.Done()

				// Create a new context with a timeout for each message.
				// Crucially, it's derived from context.Background() so it's not affected
				// by the appCtx cancellation. This allows in-flight work to complete.
				msgCtx, cancelMsg := context.WithTimeout(context.Background(), processingTimeout)
				defer cancelMsg()

				processMessage(msgCtx, sqsClient, router, &m, queueURL)
			}(msg)
		}
	}

	// After the polling loop has stopped, wait for all active workers to finish their jobs.
	log.Println("INFO: Waiting for in-flight messages to be processed...")
	wg.Wait()

	log.Println("✅ Graceful shutdown complete. All processed messages are handled.")
}

// processMessage routes, handles, and deletes a single SQS message.
func processMessage(ctx context.Context, client *sqs.SQS, r *Router, msg *types.Message, queueURL string) {
	if msg.Body == nil {
		log.Println("ERROR: Received message with empty body.")
		return
	}

	routed := r.Route(ctx, []byte(*msg.Body))

	// Log the outcome of the processing attempt.
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

	// Delete the message from the queue if the handler result indicates it should be.
	if routed.HandlerResult.ShouldDelete {
		// Use a background context for deletion as it's a critical cleanup step.
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancelDelete()

		_, err := client.DeleteMessage(deleteCtx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(queueURL),
			ReceiptHandle: msg.ReceiptHandle,
		})

		if err != nil {
			log.Printf("ERROR: Failed to delete message ID %s: %v", routed.MessageID, err)
		} else {
			log.Printf("🗑️  Deleted message ID %s", routed.MessageID)
		}
	} else {
		// If ShouldDelete is false, the message will become visible again in SQS
		// after the visibility timeout expires, allowing for a retry.
		log.Printf("🔁 RETRYING message ID %s later (visibility timeout will expire).", routed.MessageID)
	}
}
