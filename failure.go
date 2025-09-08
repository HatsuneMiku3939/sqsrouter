package sqsrouter

import "github.com/hatsunemiku3939/sqsrouter/spec"

// Re-export FailureKind typed constants for backward compatibility.
const (
	FailNone            = spec.FailNone
	FailEnvelopeSchema  = spec.FailEnvelopeSchema
	FailEnvelopeParse   = spec.FailEnvelopeParse
	FailPayloadSchema   = spec.FailPayloadSchema
	FailNoHandler       = spec.FailNoHandler
	FailHandlerError    = spec.FailHandlerError
	FailHandlerPanic    = spec.FailHandlerPanic
	FailMiddlewareError = spec.FailMiddlewareError
)
