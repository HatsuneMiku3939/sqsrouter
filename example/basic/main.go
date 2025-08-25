package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatsunemiku3939/sqsrouter"
)

// --- Schemas ---

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


// --- Constants for Message Types and Versions ---
const (
	MsgTypeUpdateUserProfile = "updateUserProfile"
	MsgVersion1_0            = "1.0"
)

// --- Message Payloads & Metadata ---

// UserProfileMessage defines the structure for the "updateUserProfile" message payload.
type UserProfileMessage struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// --- Message Handlers ---

// UpdateUserProfileV1Handler handles the logic for updating a user profile.
func UpdateUserProfileV1Handler(ctx context.Context, messageJSON []byte, metadataJSON []byte) sqsrouter.HandlerResult {
	var msg UserProfileMessage
	if err := json.Unmarshal(messageJSON, &msg); err != nil {
		// This error should theoretically not happen if schema validation is correct.
		// But as a safeguard, we handle it as a permanent failure.
		return sqsrouter.HandlerResult{ShouldDelete: true, Error: fmt.Errorf("failed to unmarshal user profile message: %w", err)}
	}

	// In a real application, this is where you would interact with a database or another service.
	log.Printf("⚙️  Processing user update for %s (ID: %s)", msg.Username, msg.UserID)

	// Simulate work that can be canceled.
	select {
	case <-time.After(2 * time.Second): // Simulate 2 seconds of work.
		log.Printf("INFO: Finished processing for user %s", msg.UserID)
		// For this example, we assume the operation always succeeds.
		return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
	case <-ctx.Done(): // This case will be hit if the processingTimeout is exceeded.
		log.Printf("WARN: Processing canceled for user %s: %v", msg.UserID, ctx.Err())
		// Return ShouldDelete: false to allow for a retry.
		return sqsrouter.HandlerResult{ShouldDelete: false, Error: ctx.Err()}
	}
}

// --- SQS Consumer Entry Point ---

func main() {
	// --- 1. Setup Context for Graceful Shutdown ---
	appCtx, cancelApp := context.WithCancel(context.Background())
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-shutdownChan
		log.Printf("🛑 Received shutdown signal: %v. Starting graceful shutdown...", sig)
		cancelApp()
	}()

	// --- 2. Load Configuration ---
	cfg, err := config.LoadDefaultConfig(appCtx)
	if err != nil {
		log.Fatalf("FATAL: Failed to load AWS config: %v", err)
	}

	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("FATAL: SQS_QUEUE_URL environment variable is not set.")
	}

	sqsClient := sqs.NewFromConfig(cfg)

	// --- 3. Setup Router and Register Handlers ---
	router, err := sqsrouter.NewRouter(envelopeSchema)
	if err != nil {
		log.Fatalf("FATAL: Could not initialize router: %v", err)
	}

	router.Register(MsgTypeUpdateUserProfile, MsgVersion1_0, UpdateUserProfileV1Handler)
	if err := router.RegisterSchema(MsgTypeUpdateUserProfile, MsgVersion1_0, userProfileSchema); err != nil {
		log.Fatalf("FATAL: Could not register schema: %v", err)
	}

	// --- 4. Setup and Start the Consumer ---
	consumer := sqsrouter.NewConsumer(sqsClient, queueURL, router)
	consumer.Start(appCtx)

	log.Println("Application has shut down.")
}
