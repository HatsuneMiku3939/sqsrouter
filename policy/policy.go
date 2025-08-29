package policy

import (
	"context"
)

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

// ImmediateDeletePolicy marks structural/permanent failures for deletion immediately.
// Middleware errors do not force deletion; handler semantics are preserved.
type ImmediateDeletePolicy struct{}

// Decide implements ImmediateDeletePolicy behavior.
func (p ImmediateDeletePolicy) Decide(_ context.Context, kind FailureKind, inner error, current Result) Result {
	switch kind {
	case FailNone:
		return current
	case FailEnvelopeSchema, FailEnvelopeParse, FailPayloadSchema, FailNoHandler, FailHandlerPanic:
		current.ShouldDelete = true
		if inner != nil && current.Error == nil {
			current.Error = inner
		}
		return current
	case FailMiddlewareError, FailHandlerError:
		if inner != nil && current.Error == nil {
			current.Error = inner
		}
		return current
	default:
		return current
	}
}

// SQSRedrivePolicy always returns ShouldDelete=false for failures so SQS redrive handles retries/DLQ.
type SQSRedrivePolicy struct{}

// Decide implements the Policy interface for SQS redrive delegation.
func (p SQSRedrivePolicy) Decide(_ context.Context, kind FailureKind, inner error, current Result) Result {
	if kind == FailNone {
		return current
	}
	current.ShouldDelete = false
	if inner != nil && current.Error == nil {
		current.Error = inner
	}
	return current
}
