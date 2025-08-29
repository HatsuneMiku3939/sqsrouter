package policy

import "context"

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
	// FailNoHandler indicates no handler was registered for the message type/version.
	FailNoHandler
	// FailHandlerError indicates the user handler returned a non-nil error.
	// Policy may choose to respect or override the handler's ShouldDelete decision.
	FailHandlerError
	// FailHandlerPanic indicates a panic occurred inside user handler or outer recovery.
	FailHandlerPanic
	// FailMiddlewareError indicates an error was returned by the middleware-wrapped core pipeline.
	FailMiddlewareError
)

// Result represents the delete decision and error to attach.
type Result struct {
	ShouldDelete bool
	Error        error
}

// Policy decides the final Result given a failure classification and current decision.
type Policy interface {
	Decide(ctx context.Context, kind FailureKind, inner error, current Result) Result
}
