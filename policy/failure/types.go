package failure

import "context"

// Kind enumerates where in the pipeline a failure occurred.
type Kind int

const (
	// FailNone indicates no failure occurred.
	FailNone Kind = iota
	// FailEnvelopeSchema indicates the outer envelope JSON failed schema validation.
	FailEnvelopeSchema
	// FailEnvelopeParse indicates the outer envelope JSON could not be parsed.
	FailEnvelopeParse
	// FailPayloadSchema indicates the inner message payload failed its registered schema validation.
	FailPayloadSchema
	// FailNoHandler indicates no handler was registered or selected for the message.
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
	Decide(ctx context.Context, kind Kind, inner error, current Result) Result
}
