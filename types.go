package sqsrouter

import (
	"sync"

	jsonschema "github.com/hatsunemiku3939/sqsrouter/internal/jsonschema"
	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// Router routes incoming messages to the correct handler based on message type and version.
// It is safe for concurrent use.
type Router struct {
	mu             sync.RWMutex
	handlers       map[string]spec.MessageHandler
	schemas        map[string]jsonschema.JSONLoader
	envelopeSchema jsonschema.JSONLoader

	middlewares   []spec.Middleware
	routingPolicy spec.RoutingPolicy
	failurePolicy spec.FailurePolicy
}
