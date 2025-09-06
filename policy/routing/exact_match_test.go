package routing

import (
    "context"
    "testing"

    "github.com/hatsunemiku3939/sqsrouter"
)

func TestExactMatchPolicy_SelectsExactKey(t *testing.T) {
    p := ExactMatchPolicy{}
    env := &sqsrouter.MessageEnvelope{MessageType: "A", MessageVersion: "v1"}
    keys := []sqsrouter.HandlerKey{"A:v0", "A:v1", "B:v1"}
    got := p.Decide(context.Background(), env, keys)
    if string(got) != "A:v1" {
        t.Fatalf("expected A:v1, got %q", got)
    }
}

func TestExactMatchPolicy_ReturnsEmptyWhenMissing(t *testing.T) {
    p := ExactMatchPolicy{}
    env := &sqsrouter.MessageEnvelope{MessageType: "A", MessageVersion: "v9"}
    keys := []sqsrouter.HandlerKey{"A:v0", "B:v1"}
    got := p.Decide(context.Background(), env, keys)
    if got != "" {
        t.Fatalf("expected empty key, got %q", got)
    }
}

