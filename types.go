package sqsrouter

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/xeipuuv/gojsonschema"
)

// MessageEnvelope is a struct to unmarshal the outer layer of an SQS message.
// It contains the routing information and the actual message payload.
type MessageEnvelope struct {
	SchemaVersion  string          `json:"schemaVersion"`
	MessageType    string          `json:"messageType"`
	MessageVersion string          `json:"messageVersion"`
	Message        json.RawMessage `json:"message"`
	Metadata       MessageMetadata `json:"metadata"`
}

// MessageMetadata holds common metadata found in every message.
type MessageMetadata struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	MessageID string `json:"messageId"`
}

// HandlerResult indicates the outcome of processing a message.
type HandlerResult struct {
	ShouldDelete bool
	Error        error
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

// RouteState carries per-message routing context through the middleware and core routing pipeline.
// It includes the raw message, parsed envelope, handler/schema resolution, and derived metadata.
type RouteState struct {
	Raw           []byte
	Envelope      *MessageEnvelope
	HandlerKey    string
	HandlerExists bool
	SchemaExists  bool
	Metadata      *MessageMetadata
	Handler       MessageHandler
	Schema        gojsonschema.JSONLoader
}

// HandlerFunc is the function signature wrapped by middlewares.
type HandlerFunc func(ctx context.Context, state *RouteState) (RoutedResult, error)

// Middleware composes cross-cutting concerns around the routing core, forming a chain of HandlerFunc.
// Typical use cases: logging, tracing, metrics, auth, and failure policy adjustments.
type Middleware func(next HandlerFunc) HandlerFunc

// Router routes incoming messages to the correct handler based on message type and version.
// It is safe for concurrent use.
type Router struct {
	mu             sync.RWMutex
	handlers       map[string]MessageHandler
	schemas        map[string]gojsonschema.JSONLoader
	envelopeSchema gojsonschema.JSONLoader

	middlewares []Middleware
	failFast    bool
}

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
