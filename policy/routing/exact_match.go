package routing

import (
	"context"

	"github.com/hatsunemiku3939/sqsrouter"
)

// ExactMatchPolicy selects handler strictly matching messageType:messageVersion.
type ExactMatchPolicy struct{}

// Decide returns the exact key if present; otherwise empty.
func (ExactMatchPolicy) Decide(_ context.Context, envelope *sqsrouter.MessageEnvelope, available []sqsrouter.HandlerKey) sqsrouter.HandlerKey { //nolint:revive
	want := sqsrouter.HandlerKey(envelope.MessageType + ":" + envelope.MessageVersion)
	for _, k := range available {
		if k == want {
			return k
		}
	}
	return ""
}
