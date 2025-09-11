package routing

import (
	"context"

	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// ExactMatchPolicy selects handler strictly matching messageType:messageVersion.
type ExactMatchPolicy struct{}

// Decide returns the exact key if present; otherwise empty.
func (ExactMatchPolicy) Decide(_ context.Context, envelope *spec.MessageEnvelope, available []spec.HandlerKey) spec.HandlerKey { //nolint:revive
	want := spec.HandlerKey(makeKey(envelope.MessageType, envelope.MessageVersion))
	for _, k := range available {
		if k == want {
			return k
		}
	}
	return ""
}

// makeKey creates a consistent key for maps from message type and version.
func makeKey(messageType, messageVersion string) string {
	return messageType + ":" + messageVersion
}
