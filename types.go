package sqsrouter

import (
	"sync"

	jsonschema "github.com/hatsunemiku3939/sqsrouter/internal/jsonschema"
	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// Re-export public API types from spec for backward compatibility.
type (
	MessageEnvelope = spec.MessageEnvelope
	MessageMetadata = spec.MessageMetadata
	HandlerResult   = spec.HandlerResult
	RoutedResult    = spec.RoutedResult
	MessageHandler  = spec.MessageHandler
	RouteState      = spec.RouteState
	HandlerFunc     = spec.HandlerFunc
	Middleware      = spec.Middleware
	HandlerKey      = spec.HandlerKey
	RoutingPolicy   = spec.RoutingPolicy
	FailureKind     = spec.FailureKind
	FailureResult   = spec.FailureResult
	FailurePolicy   = spec.FailurePolicy
)

// Router routes incoming messages to the correct handler based on message type and version.
// It is safe for concurrent use.
type Router struct {
	mu             sync.RWMutex
	handlers       map[string]MessageHandler
	schemas        map[string]jsonschema.JSONLoader
	envelopeSchema jsonschema.JSONLoader

	middlewares   []Middleware
	routingPolicy RoutingPolicy
	failurePolicy FailurePolicy
}
