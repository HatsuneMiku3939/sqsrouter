package spec

import (
	"context"
	"encoding/json"
	"time"

	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
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
// It receives the message payload as raw JSON bytes. Metadata and SQS attributes
// are accessible via GetMessageContext(ctx).
type MessageHandler func(ctx context.Context, messageJSON []byte) HandlerResult

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

// Middleware composes cross-cutting concerns around the routing core.
type Middleware func(next HandlerFunc) HandlerFunc

// HandlerKey is the unique identifier for a registered handler (e.g., "messageType:messageVersion").
type HandlerKey string

// MessageContext holds key attributes of an SQS message and envelope metadata.
// Handlers and middlewares can access it via GetMessageContext(ctx).
type MessageContext struct {
	// --- From SQS System Attributes ---
	ReceiveCount           int
	SentTimestamp          time.Time
	FirstReceiveTimestamp  time.Time
	MessageGroupID         string
	MessageDeduplicationID string

	// --- From SQS Message Attributes ---
	CustomAttributes map[string]sqstypes.MessageAttributeValue

	// --- From Message Envelope Metadata ---
	MessageID string
	Source    string
	Timestamp string // Application-level timestamp
}

// messageContextKeyType is an unexported type to avoid context key collisions.
type messageContextKeyType struct{}

var messageContextKey = messageContextKeyType{}

// WithMessageContext attaches MessageContext to a context and returns the derived context.
func WithMessageContext(ctx context.Context, mc *MessageContext) context.Context {
	if mc == nil {
		return ctx
	}
	return context.WithValue(ctx, messageContextKey, mc)
}

// GetMessageContext extracts the MessageContext from a context.
func GetMessageContext(ctx context.Context) (*MessageContext, bool) {
	if ctx == nil {
		return nil, false
	}
	mc, ok := ctx.Value(messageContextKey).(*MessageContext)
	return mc, ok
}
