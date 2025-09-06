package sqsrouter

import "context"

// ExactMatchPolicy selects handler strictly matching messageType:messageVersion.
type ExactMatchPolicy struct{}

// Decide returns the exact key if present; otherwise empty (no selection).
func (ExactMatchPolicy) Decide(_ context.Context, envelope *MessageEnvelope, available []HandlerKey) HandlerKey {
	want := HandlerKey(envelope.MessageType + ":" + envelope.MessageVersion)
	for _, k := range available {
		if k == want {
			return k
		}
	}
	return ""
}
