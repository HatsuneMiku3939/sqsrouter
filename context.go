package sqsrouter

import (
	"context"

	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// messageContextKeyType is an unexported type to avoid context key collisions.
type messageContextKeyType struct{}

var messageContextKey = messageContextKeyType{}

// WithMessageContext attaches MessageContext to a context and returns the derived context.
func WithMessageContext(ctx context.Context, mc *spec.MessageContext) context.Context {
	if mc == nil {
		return ctx
	}
	return context.WithValue(ctx, messageContextKey, mc)
}

// GetMessageContext extracts the MessageContext from a context.
func GetMessageContext(ctx context.Context) (*spec.MessageContext, bool) {
	if ctx == nil {
		return nil, false
	}
	mc, ok := ctx.Value(messageContextKey).(*spec.MessageContext)
	return mc, ok
}
