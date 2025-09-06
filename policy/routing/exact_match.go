package routing

import (
	"context"

	stypes "github.com/hatsunemiku3939/sqsrouter/types"
)

// ExactMatchPolicy selects handler strictly matching messageType:messageVersion.
type ExactMatchPolicy struct{}

// Decide returns the exact key if present; otherwise empty.
func (ExactMatchPolicy) Decide(_ context.Context, envelope *stypes.MessageEnvelope, available []stypes.HandlerKey) stypes.HandlerKey { //nolint:revive
	want := stypes.HandlerKey(envelope.MessageType + ":" + envelope.MessageVersion)
	for _, k := range available {
		if k == want {
			return k
		}
	}
	return ""
}
