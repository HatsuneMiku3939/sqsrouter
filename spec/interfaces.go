package spec

import "context"

// RoutingPolicy decides which handler should process an incoming message.
// Implementations may perform exact match, version fallback, A/B testing, etc.
// Returning an empty HandlerKey means no handler selected.
type RoutingPolicy interface {
	Decide(ctx context.Context, envelope *MessageEnvelope, availableHandlers []HandlerKey) HandlerKey
}

// FailureKind enumerates where in the pipeline a failure occurred.
type FailureKind int

const (
	// FailNone indicates no failure occurred.
	FailNone FailureKind = iota
	// FailEnvelopeSchema indicates the outer envelope JSON failed schema validation.
	FailEnvelopeSchema
	// FailEnvelopeParse indicates the outer envelope JSON could not be parsed.
	FailEnvelopeParse
	// FailPayloadSchema indicates the inner message payload failed its registered schema validation.
	FailPayloadSchema
	// FailNoHandler indicates no handler was registered or selected for the message.
	FailNoHandler
	// FailHandlerError indicates the user handler returned a non-nil error.
	FailHandlerError
	// FailHandlerPanic indicates a panic occurred inside user handler or outer recovery.
	FailHandlerPanic
	// FailMiddlewareError indicates an error was returned by the middleware-wrapped core pipeline.
	FailMiddlewareError
)

// FailureResult represents the delete decision and error to attach.
type FailureResult struct {
	ShouldDelete bool
	Error        error
}

// FailurePolicy decides the final FailureResult given a failure classification and current decision.
type FailurePolicy interface {
	Decide(ctx context.Context, kind FailureKind, inner error, current FailureResult) FailureResult
}
