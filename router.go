package sqsrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

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

var EnvelopeSchema = `{
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
