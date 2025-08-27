package sqsrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

// --- Handler & Router Logic ---

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

// NewRouter creates and initializes a new Router with a given envelope schema.
func NewRouter(envelopeSchema string) (*Router, error) {
	loader := gojsonschema.NewStringLoader(envelopeSchema)
	if _, err := gojsonschema.NewSchema(loader); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnvelopeSchema, err)
	}

	return &Router{
		handlers:       make(map[string]MessageHandler),
		schemas:        make(map[string]gojsonschema.JSONLoader),
		envelopeSchema: loader,
		middlewares:    nil,
		failFast:       false,
	}, nil
}

func (r *Router) WithFailFast(v bool) {
	r.mu.Lock()
	r.failFast = v
	r.mu.Unlock()
}

func (r *Router) Use(mw ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(mw) == 0 {
		return
	}
	newSlice := make([]Middleware, 0, len(r.middlewares)+len(mw))
	newSlice = append(newSlice, r.middlewares...)
	newSlice = append(newSlice, mw...)
	r.middlewares = newSlice
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
	if _, err := gojsonschema.NewSchema(loader); err != nil {
		return fmt.Errorf("%w for %s:%s: %v", ErrInvalidSchema, messageType, messageVersion, err)
	}

	key := makeKey(messageType, messageVersion)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemas[key] = loader
	return nil
}

func formatSchemaError(result *gojsonschema.Result, err error) error {
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaValidationSystem, err)
	}
	if result.Valid() {
		return nil
	}

	var errMsg string
	for _, desc := range result.Errors() {
		errMsg += fmt.Sprintf("- %s; ", desc)
	}
	return fmt.Errorf("%w: %s", ErrSchemaValidationFailed, errMsg)
}

func (r *Router) coreRoute(ctx context.Context, state *RouteState) (RoutedResult, error) {
	res, err := gojsonschema.Validate(r.envelopeSchema, gojsonschema.NewBytesLoader(state.Raw))
	if validationErr := formatSchemaError(res, err); validationErr != nil {
		rr := RoutedResult{
			MessageType:    "unknown",
			MessageVersion: "unknown",
			HandlerResult: HandlerResult{
				ShouldDelete: true,
				Error:        fmt.Errorf("%w: %v", ErrInvalidEnvelope, validationErr),
			},
		}
		return rr, rr.HandlerResult.Error
	}

	var envelope MessageEnvelope
	if err := json.Unmarshal(state.Raw, &envelope); err != nil {
		rr := RoutedResult{
			MessageType:    "unknown",
			MessageVersion: "unknown",
			HandlerResult: HandlerResult{
				ShouldDelete: true,
				Error:        fmt.Errorf("%w: %v", ErrFailedToParseEnvelope, err),
			},
		}
		return rr, rr.HandlerResult.Error
	}
	state.Envelope = &envelope
	state.HandlerKey = makeKey(envelope.MessageType, envelope.MessageVersion)

	r.mu.RLock()
	handler, handlerExists := r.handlers[state.HandlerKey]
	schemaLoader, schemaExists := r.schemas[state.HandlerKey]
	r.mu.RUnlock()
	state.Handler = handler
	state.Schema = schemaLoader
	state.HandlerExists = handlerExists
	state.SchemaExists = schemaExists

	if schemaExists {
		res, err := gojsonschema.Validate(schemaLoader, gojsonschema.NewBytesLoader(envelope.Message))
		if validationErr := formatSchemaError(res, err); validationErr != nil {
			rr := RoutedResult{
				MessageType:    envelope.MessageType,
				MessageVersion: envelope.MessageVersion,
				HandlerResult: HandlerResult{
					ShouldDelete: true,
					Error:        fmt.Errorf("%w: %v", ErrInvalidMessagePayload, validationErr),
				},
			}
			return rr, rr.HandlerResult.Error
		}
	}

	if !handlerExists {
		rr := RoutedResult{
			MessageType:    envelope.MessageType,
			MessageVersion: envelope.MessageVersion,
			HandlerResult: HandlerResult{
				ShouldDelete: true,
				Error:        fmt.Errorf("%w for %s", ErrNoHandlerRegistered, state.HandlerKey),
			},
		}
		return rr, rr.HandlerResult.Error
	}

	meta := envelope.Metadata
	state.Metadata = &meta

	metaJSON, _ := json.Marshal(meta)
	handlerResult := handler(ctx, envelope.Message, metaJSON)

	rr := RoutedResult{
		MessageType:    envelope.MessageType,
		MessageVersion: envelope.MessageVersion,
		HandlerResult:  handlerResult,
		MessageID:      meta.MessageID,
		Timestamp:      meta.Timestamp,
	}
	return rr, nil
}

// Route validates and dispatches a raw message to the appropriate registered handler.
func (r *Router) Route(ctx context.Context, rawMessage []byte) RoutedResult {
	state := &RouteState{Raw: rawMessage}

	r.mu.RLock()
	mws := r.middlewares
	failFast := r.failFast
	r.mu.RUnlock()

	core := func(ctx context.Context, s *RouteState) (RoutedResult, error) {
		return r.coreRoute(ctx, s)
	}

	for i := len(mws) - 1; i >= 0; i-- {
		core = mws[i](core)
	}

	routed, err := core(ctx, state)
	if err != nil {
		if failFast {
			return RoutedResult{
				MessageType:    routed.MessageType,
				MessageVersion: routed.MessageVersion,
				HandlerResult: HandlerResult{
					ShouldDelete: true,
					Error:        fmt.Errorf("%w: %v", ErrMiddleware, err),
				},
				MessageID: routed.MessageID,
				Timestamp: routed.Timestamp,
			}
		}
		return routed
	}
	return routed
}

// --- Schemas ---

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
