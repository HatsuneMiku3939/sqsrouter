package sqsrouter

import "github.com/hatsunemiku3939/sqsrouter/spec"

// RouterOption configures a Router at construction time.
type RouterOption func(*Router)

// WithFailurePolicy sets a custom failure policy for the Router.
func WithFailurePolicy(p spec.FailurePolicy) RouterOption {
	return func(r *Router) { r.failurePolicy = p }
}

// WithRoutingPolicy sets a custom routing policy for the Router.
func WithRoutingPolicy(p spec.RoutingPolicy) RouterOption {
	return func(r *Router) { r.routingPolicy = p }
}
