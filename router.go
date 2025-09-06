package sqsrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hatsunemiku3939/sqsrouter/internal/jsonschema"
	failure "github.com/hatsunemiku3939/sqsrouter/policy/failure"
)

// coreFailureErr is used to propagate a failure signal through middlewares
// while avoiding double policy application in Route. It wraps the original cause
// and tags it with the FailureKind.
type coreFailureErr struct {
	kind  failure.Kind
	cause error
}

// Error implements error interface.
func (e coreFailureErr) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return "core failure"
}

// Unwrap returns the underlying error cause.
func (e coreFailureErr) Unwrap() error { return e.cause }

// NewRouter creates and initializes a new Router with a given envelope schema.
func NewRouter(envelopeSchema string, opts ...RouterOption) (*Router, error) {
	loader := jsonschema.NewStringLoader(envelopeSchema)
	if _, err := jsonschema.NewSchema(loader); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnvelopeSchema, err)
	}

	r := &Router{
		handlers:       make(map[string]MessageHandler),
		schemas:        make(map[string]jsonschema.JSONLoader),
		envelopeSchema: loader,
		middlewares:    nil,
		routingPolicy:  nil,
		failurePolicy:  failure.ImmediateDeletePolicy{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
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
// Behavior:
//   - On failures within core routing, the Policy is consulted immediately and the decided RoutedResult is returned with a nil error.
//   - Any panics from user handlers are not recovered here; they bubble up to the outer Route guard which maps them to FailHandlerPanic via Policy.
func (r *Router) coreRoute(ctx context.Context, state *RouteState) (RoutedResult, error) {
	// Step 1: Validate the envelope structure before any parsing.
	res, err := jsonschema.Validate(r.envelopeSchema, jsonschema.NewBytesLoader(state.Raw))
	if validationErr := jsonschema.FormatErrors(res, err); validationErr != nil {
		rr := RoutedResult{
			MessageType:    "unknown",
			MessageVersion: "unknown",
			HandlerResult: HandlerResult{
				ShouldDelete: false,
				Error:        fmt.Errorf("%w: %v", ErrInvalidEnvelope, validationErr),
			},
		}
		pr := r.failurePolicy.Decide(ctx, failure.FailEnvelopeSchema, rr.HandlerResult.Error, failure.Result{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
		rr.HandlerResult.ShouldDelete = pr.ShouldDelete
		rr.HandlerResult.Error = pr.Error
		return rr, coreFailureErr{kind: failure.FailEnvelopeSchema, cause: rr.HandlerResult.Error}
	}

	// Step 2: Parse the envelope to extract routing metadata and payload.
	var envelope MessageEnvelope
	if err := json.Unmarshal(state.Raw, &envelope); err != nil {
		rr := RoutedResult{
			MessageType:    "unknown",
			MessageVersion: "unknown",
			HandlerResult: HandlerResult{
				ShouldDelete: false,
				Error:        fmt.Errorf("%w: %v", ErrFailedToParseEnvelope, err),
			},
		}
		pr := r.failurePolicy.Decide(ctx, failure.FailEnvelopeParse, rr.HandlerResult.Error, failure.Result{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
		rr.HandlerResult.ShouldDelete = pr.ShouldDelete
		rr.HandlerResult.Error = pr.Error
		return rr, coreFailureErr{kind: failure.FailEnvelopeParse, cause: rr.HandlerResult.Error}
	}
	state.Envelope = &envelope
	// Decide handler using routing policy (default exact-match when nil).
	var decided HandlerKey
	if r.routingPolicy == nil {
		decided = HandlerKey(makeKey(envelope.MessageType, envelope.MessageVersion))
	} else {
		r.mu.RLock()
		available := make([]HandlerKey, 0, len(r.handlers))
		for k := range r.handlers {
			available = append(available, HandlerKey(k))
		}
		r.mu.RUnlock()
		decided = r.routingPolicy.Decide(ctx, &envelope, available)
	}
	state.HandlerKey = string(decided)

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
					ShouldDelete: false,
					Error:        fmt.Errorf("%w: %v", ErrInvalidMessagePayload, validationErr),
				},
				MessageID: envelope.Metadata.MessageID,
				Timestamp: envelope.Metadata.Timestamp,
			}
			pr := r.failurePolicy.Decide(ctx, failure.FailPayloadSchema, rr.HandlerResult.Error, failure.Result{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
			rr.HandlerResult.ShouldDelete = pr.ShouldDelete
			rr.HandlerResult.Error = pr.Error
			return rr, coreFailureErr{kind: failure.FailPayloadSchema, cause: rr.HandlerResult.Error}
		}
	}

	// Step 5: Ensure a handler exists for the resolved key; otherwise fail fast for this message.
	if !handlerExists {
		rr := RoutedResult{
			MessageType:    envelope.MessageType,
			MessageVersion: envelope.MessageVersion,
			HandlerResult: HandlerResult{
				ShouldDelete: false,
				Error:        fmt.Errorf("%w for %s", ErrNoHandlerRegistered, state.HandlerKey),
			},
			MessageID: envelope.Metadata.MessageID,
			Timestamp: envelope.Metadata.Timestamp,
		}
		pr := r.failurePolicy.Decide(ctx, failure.FailNoHandler, rr.HandlerResult.Error, failure.Result{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
		rr.HandlerResult.ShouldDelete = pr.ShouldDelete
		rr.HandlerResult.Error = pr.Error
		return rr, coreFailureErr{kind: failure.FailNoHandler, cause: rr.HandlerResult.Error}
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
	// Invoke the resolved handler with payload and metadata.
	// Do not recover here; allow panics to bubble to Route, which maps them to FailHandlerPanic via Policy.
	handlerResult := handler(ctx, envelope.Message, metaJSON)

	// Assemble the routed result from handler output.
	rr := RoutedResult{
		MessageType:    envelope.MessageType,
		MessageVersion: envelope.MessageVersion,
		HandlerResult:  handlerResult,
		MessageID:      meta.MessageID,
		Timestamp:      meta.Timestamp,
	}
	// If handler returned an error, consult Policy so it can be the final decider.
	if handlerResult.Error != nil {
		pr := r.failurePolicy.Decide(ctx, failure.FailHandlerError, handlerResult.Error, failure.Result{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
		rr.HandlerResult.ShouldDelete = pr.ShouldDelete
		rr.HandlerResult.Error = pr.Error
		return rr, nil
	}
	// No error: return as-is.
	return rr, nil
}

// Route validates and dispatches a raw message to the appropriate registered handler.
func (r *Router) Route(ctx context.Context, rawMessage []byte) RoutedResult {
	// Prepare per-message state container.
	state := &RouteState{Raw: rawMessage}

	r.mu.RLock()
	mws := r.middlewares
	r.mu.RUnlock()

	core := func(ctx context.Context, s *RouteState) (RoutedResult, error) {
		return r.coreRoute(ctx, s)
	}

	for i := len(mws) - 1; i >= 0; i-- {
		core = mws[i](core)
	}

	// Execute the middleware-wrapped core with an outermost panic recovery guard.
	var routed RoutedResult
	var err error
	panicOccurred := false

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
				tmp := RoutedResult{
					MessageType:    msgType,
					MessageVersion: msgVer,
					HandlerResult: HandlerResult{
						ShouldDelete: false,
						Error:        fmt.Errorf("%w: %v", ErrPanic, rec),
					},
					MessageID: msgID,
					Timestamp: timestamp,
				}

				pr := r.failurePolicy.Decide(ctx, failure.FailHandlerPanic, tmp.HandlerResult.Error, failure.Result{ShouldDelete: tmp.HandlerResult.ShouldDelete, Error: tmp.HandlerResult.Error})
				tmp.HandlerResult.ShouldDelete = pr.ShouldDelete
				tmp.HandlerResult.Error = pr.Error
				routed = tmp

				err = nil
				panicOccurred = true
			}
		}()

		// Execute the wrapped core; this may return an error (handled below) or panic (handled by defer above).
		routed, err = core(ctx, state)
	}()

	if !panicOccurred && err != nil {
		// If the error originated from coreRoute (already policy-decided), do not re-apply policy.
		var cfe coreFailureErr
		if errors.As(err, &cfe) {
			return routed
		}
		// Else, treat as middleware error and consult policy once.
		pr := r.failurePolicy.Decide(ctx, failure.FailMiddlewareError, err, failure.Result{ShouldDelete: routed.HandlerResult.ShouldDelete, Error: routed.HandlerResult.Error})
		routed.HandlerResult.ShouldDelete = pr.ShouldDelete
		routed.HandlerResult.Error = pr.Error
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
