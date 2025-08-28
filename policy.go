package sqsrouter

import (
	"context"
)

type FailureKind int

const (
	FailNone FailureKind = iota
	FailEnvelopeSchema
	FailEnvelopeParse
	FailPayloadSchema
	FailNoHandler
	FailHandlerPanic
	FailMiddlewareError
)

type Policy interface {
	Decide(ctx context.Context, st *RouteState, kind FailureKind, inner error, current RoutedResult) RoutedResult
}

type RouterOption func(*Router)

func WithPolicy(p Policy) RouterOption {
	return func(r *Router) {
		r.policy = p
	}
}

type DLQDefaultPolicy struct{}

func (p DLQDefaultPolicy) Decide(_ context.Context, _ *RouteState, kind FailureKind, inner error, rr RoutedResult) RoutedResult {
	switch kind {
	case FailNone:
		return rr
	case FailEnvelopeSchema, FailEnvelopeParse, FailPayloadSchema, FailNoHandler, FailHandlerPanic:
		rr.HandlerResult.ShouldDelete = true
		if inner != nil && rr.HandlerResult.Error == nil {
			rr.HandlerResult.Error = inner
		}
		return rr
	case FailMiddlewareError:
		if inner != nil && rr.HandlerResult.Error == nil {
			rr.HandlerResult.Error = inner
		}
		return rr
	default:
		return rr
	}
}
