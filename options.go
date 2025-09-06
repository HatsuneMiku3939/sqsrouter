package sqsrouter

// RouterOption configures a Router at construction time.
type RouterOption func(*Router)

// WithFailurePolicy sets a custom failure policy for the Router.
func WithFailurePolicy(p FailurePolicy) RouterOption {
	return func(r *Router) { r.failurePolicy = p }
}

// WithRoutingPolicy sets a custom routing policy for the Router.
func WithRoutingPolicy(p RoutingPolicy) RouterOption {
	return func(r *Router) { r.routingPolicy = p }
}
