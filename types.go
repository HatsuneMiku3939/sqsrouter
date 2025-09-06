package sqsrouter

import (
	"context"
	"sync"

	failure "github.com/hatsunemiku3939/sqsrouter/policy/failure"
	stypes "github.com/hatsunemiku3939/sqsrouter/types"
	"github.com/xeipuuv/gojsonschema"
)

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
	Envelope      *stypes.MessageEnvelope
	HandlerKey    string
	HandlerExists bool
	SchemaExists  bool
	Metadata      *stypes.MessageMetadata
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

	middlewares   []Middleware
	routingPolicy stypes.RoutingPolicy
	failurePolicy failure.Policy
}

// (no consumer types here; moved to consumer package)

// (routing policy and shared message types moved to package types)
