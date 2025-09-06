package sqsrouter

import (
	failure "github.com/hatsunemiku3939/sqsrouter/policy/failure"
	stypes "github.com/hatsunemiku3939/sqsrouter/types"
)

// RouterOption configures a Router at construction time.
type RouterOption func(*Router)

// WithFailurePolicy sets a custom failure policy for the Router.
func WithFailurePolicy(p failure.Policy) RouterOption {
	return func(r *Router) { r.failurePolicy = p }
}

// WithRoutingPolicy sets a custom routing policy for the Router.
func WithRoutingPolicy(p stypes.RoutingPolicy) RouterOption {
	return func(r *Router) { r.routingPolicy = p }
}
