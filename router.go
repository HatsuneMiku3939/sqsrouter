package sqsrouter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hatsunemiku3939/sqsrouter/pkg/jsonschema"
)

// NewRouter creates and initializes a new Router with a given envelope schema.
func NewRouter(envelopeSchema string) (*Router, error) {
	loader := jsonschema.NewStringLoader(envelopeSchema)
	if _, err := jsonschema.NewSchema(loader); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnvelopeSchema, err)
	}

	return &Router{
		handlers:       make(map[string]MessageHandler),
		schemas:        make(map[string]jsonschema.JSONLoader),
		envelopeSchema: loader,
		middlewares:    nil,
		failFast:       false,
	}, nil
}

// WithFailFast toggles the router's fail-fast behavior.
// When set to true, Route will return a result that requests deletion
// if any middleware-wrapped core handler returns an error, wrapping it
// with ErrMiddleware. This method is concurrency-safe.
func (r *Router) WithFailFast(v bool) {
	r.mu.Lock()
	r.failFast = v
	r.mu.Unlock()
}

// Use appends one or more middlewares to the router.
// Middlewares are applied in reverse registration order (last added runs first)
// when wrapping the core routing function in Route. Concurrency-safe.
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
	loader := jsonschema.NewStringLoader(schema)
	if _, err := jsonschema.NewSchema(loader); err != nil {
		return fmt.Errorf("%w for %s:%s: %v", ErrInvalidSchema, messageType, messageVersion, err)
	}

	key := makeKey(messageType, messageVersion)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemas[key] = loader
	return nil
}

// coreRoute executes the core routing pipeline without middleware.
// Steps:
//  1. Validate the raw envelope against the configured envelope schema. (important-comment)
//  2. Unmarshal the envelope and derive the handler key.
//  3. Resolve the registered handler and optional payload schema.
//  4. If a schema exists, validate the message payload.
//  5. Marshal metadata and invoke the resolved handler. (important-comment)
//
// It returns a RoutedResult and an error when validation or resolution fails.
// The outer Route method interprets the error according to the fail-fast policy.
func (r *Router) coreRoute(ctx context.Context, state *RouteState) (RoutedResult, error) {
	// Step 1: Validate the envelope structure before any parsing.
	res, err := jsonschema.Validate(r.envelopeSchema, jsonschema.NewBytesLoader(state.Raw))
	if validationErr := jsonschema.FormatErrors(res, err); validationErr != nil {
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

	// Step 2: Parse the envelope to extract routing metadata and payload.
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

	// Step 3: Resolve handler and optional payload schema under read lock.
	r.mu.RLock()
	handler, handlerExists := r.handlers[state.HandlerKey]
	schemaLoader, schemaExists := r.schemas[state.HandlerKey]
	r.mu.RUnlock()
	state.Handler = handler
	state.Schema = schemaLoader
	state.HandlerExists = handlerExists
	state.SchemaExists = schemaExists

	// Step 4: If a schema is registered, validate the message payload.
	if schemaExists {
		res, err := jsonschema.Validate(schemaLoader, jsonschema.NewBytesLoader(envelope.Message))
		if validationErr := jsonschema.FormatErrors(res, err); validationErr != nil {
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

	// Step 5: Ensure a handler exists for the resolved key; otherwise fail fast for this message.
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

	// Prepare metadata for the handler invocation.
	meta := envelope.Metadata
	state.Metadata = &meta

	// Marshal metadata to JSON so handler signature remains stable and decoupled.
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		rr := RoutedResult{
			MessageType:    envelope.MessageType,
			MessageVersion: envelope.MessageVersion,
			HandlerResult: HandlerResult{
				ShouldDelete: true,
				Error:        fmt.Errorf("failed to marshal metadata: %w", err),
			},
		}
		return rr, rr.HandlerResult.Error
	}
	// Protect handler invocation with panic recovery.
	// Any panic raised by user-defined handlers will be recovered and converted into a HandlerResult (ShouldDelete=true).
	// This ensures the router remains resilient and surfaces the failure through the normal error channel.


	// Invoke the resolved handler with payload and metadata.
	var handlerResult HandlerResult
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				handlerResult = HandlerResult{
					ShouldDelete: true,
					Error:        fmt.Errorf("%w: %v", ErrPanic, rec),
				}
			}
		}()
		handlerResult = handler(ctx, envelope.Message, metaJSON)
	}()

	// Assemble and return the final routed result.
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
	// Execute the middleware-wrapped core with an outermost panic recovery.
	// If any middleware or the core routing panics, we recover here and convert it into an error result.
	// The constructed RoutedResult uses "unknown" placeholders when the envelope was not yet parsed.

		core = mws[i](core)
	}

	var routed RoutedResult
	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				msgType := "unknown"
				msgVer := "unknown"
				msgID := ""
				timestamp := ""
				if state.Envelope != nil {
					msgType = state.Envelope.MessageType
					msgVer = state.Envelope.MessageVersion
					msgID = state.Envelope.Metadata.MessageID
					timestamp = state.Envelope.Metadata.Timestamp
				}
				routed = RoutedResult{
					MessageType:    msgType,
					MessageVersion: msgVer,
					HandlerResult: HandlerResult{
						ShouldDelete: true,
						Error:        fmt.Errorf("%w: %v", ErrPanic, rec),
					},
					MessageID: msgID,
					Timestamp: timestamp,
				}
				err = routed.HandlerResult.Error
			}
		}()
		routed, err = core(ctx, state)
	}()

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
