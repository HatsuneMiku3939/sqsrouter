package sqsrouter

import "github.com/hatsunemiku3939/sqsrouter/policy"

// RouterOption configures a Router at construction time.
type RouterOption func(*Router)

// WithPolicy sets a custom failure policy for the Router.
func WithPolicy(p policy.Policy) RouterOption {
	return func(r *Router) { r.policy = p }
}
