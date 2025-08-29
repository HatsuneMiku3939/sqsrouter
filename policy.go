package sqsrouter

import (
	"context"
)

type FailureKind int

const (
    // FailNone indicates no failure occurred.
    FailNone FailureKind = iota
	// FailEnvelopeSchema indicates the outer envelope JSON failed schema validation. (important-comment)
	FailEnvelopeSchema
	// FailEnvelopeParse indicates the outer envelope JSON could not be parsed at all.
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

type Policy interface {
	// Decide returns the final RoutedResult to use, given the failure kind, the original error,
	// and the current RoutedResult shape constructed by the router. Implementations may toggle
	// ShouldDelete and/or attach the inner error. This is invoked uniformly at all failure points.
	Decide(ctx context.Context, st *RouteState, kind FailureKind, inner error, current RoutedResult) RoutedResult
}

type RouterOption func(*Router)

// WithPolicy sets a custom Policy on the Router at construction time.
// Example: r, _ := NewRouter(EnvelopeSchema, WithPolicy(MyPolicy{}))
func WithPolicy(p Policy) RouterOption {
	return func(r *Router) {
		r.policy = p
	}
}

// Behavior:
// - Structural/permanent failures (envelope schema/parse, payload schema, no handler, handler panic) => delete immediately.
// - Middleware errors => do not force delete; allow retry to respect handler semantics.
type ImmediateDeletePolicy struct{}

// Decide implements the default policy described above.
// Key decisions:
// - For structural/permanent failures, mark ShouldDelete=true and attach inner error if not already present.
// - For middleware errors, preserve ShouldDelete as-is (typically false) and attach inner error if missing.
func (p ImmediateDeletePolicy) Decide(_ context.Context, _ *RouteState, kind FailureKind, inner error, rr RoutedResult) RoutedResult {
    switch kind {
    case FailNone:
        return rr
    case FailEnvelopeSchema, FailEnvelopeParse, FailPayloadSchema, FailNoHandler, FailHandlerPanic:
        rr.HandlerResult.ShouldDelete = true
        if inner != nil && rr.HandlerResult.Error == nil {
            rr.HandlerResult.Error = inner
        }
        return rr
    case FailMiddlewareError, FailHandlerError:
        if inner != nil && rr.HandlerResult.Error == nil {
            rr.HandlerResult.Error = inner
        }
        return rr
    default:
        return rr
    }
}

// SQSRedrivePolicy delegates failure handling to SQS redrive.
type SQSRedrivePolicy struct{}

// Decide implements the Policy interface for SQS redrive delegation.
// Behavior:
func (p SQSRedrivePolicy) Decide(_ context.Context, _ *RouteState, kind FailureKind, inner error, rr RoutedResult) RoutedResult {
	if kind == FailNone {
		return rr
	}
	rr.HandlerResult.ShouldDelete = false
	if inner != nil && rr.HandlerResult.Error == nil {
		rr.HandlerResult.Error = inner
	}
	return rr
}
