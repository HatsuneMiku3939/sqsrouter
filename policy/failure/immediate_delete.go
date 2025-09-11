package failure

import (
	"context"

	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// ImmediateDeletePolicy marks structural/permanent failures for deletion immediately.
// Middleware errors do not force deletion; handler semantics are preserved.
type ImmediateDeletePolicy struct{}

// Decide implements ImmediateDeletePolicy behavior.
func (p ImmediateDeletePolicy) Decide(_ context.Context, kind spec.FailureKind, inner error, current spec.FailureResult) spec.FailureResult {
	switch kind {
	case spec.FailNone:
		return current
	case spec.FailEnvelopeSchema, spec.FailEnvelopeParse, spec.FailPayloadSchema, spec.FailNoHandler, spec.FailHandlerPanic:
		current.ShouldDelete = true
		if inner != nil && current.Error == nil {
			current.Error = inner
		}
		return current
	case spec.FailMiddlewareError, spec.FailHandlerError:
		if inner != nil && current.Error == nil {
			current.Error = inner
		}
		return current
	default:
		return current
	}
}
