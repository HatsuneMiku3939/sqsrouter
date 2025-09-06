package types

import (
	"context"
	"encoding/json"
)

// MessageEnvelope is a struct to unmarshal the outer layer of an SQS message.
// It contains the routing information and the actual message payload.
type MessageEnvelope struct {
	SchemaVersion  string          `json:"schemaVersion"`
	MessageType    string          `json:"messageType"`
	MessageVersion string          `json:"messageVersion"`
	Message        json.RawMessage `json:"message"`
	Metadata       MessageMetadata `json:"metadata"`
}

// MessageMetadata holds common metadata found in every message.
type MessageMetadata struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	MessageID string `json:"messageId"`
}

// HandlerKey is the unique identifier for a registered handler (e.g., "messageType:messageVersion").
type HandlerKey string

// RoutingPolicy decides which handler should process an incoming message.
// Implementations may perform exact match, version fallback, A/B testing, etc.
// Returning an empty HandlerKey means no handler selected.
type RoutingPolicy interface {
	Decide(ctx context.Context, envelope *MessageEnvelope, availableHandlers []HandlerKey) HandlerKey
}
