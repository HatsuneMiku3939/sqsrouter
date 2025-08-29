package policy

import "context"

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
