package sqsrouter

import (
    "context"
    "testing"
)

// testRoutingPolicy routes any version to v1 if available.
type testRoutingPolicy struct{}

func (testRoutingPolicy) Decide(_ context.Context, env *MessageEnvelope, available []HandlerKey) HandlerKey { //nolint:revive
    // prefer exact match
    exact := HandlerKey(env.MessageType + ":" + env.MessageVersion)
    for _, k := range available {
        if k == exact {
            return k
        }
    }
    // fallback to v1
    fallback := HandlerKey(env.MessageType + ":v1")
    for _, k := range available {
        if k == fallback {
            return k
        }
    }
    return ""
}

func TestRoutingPolicy_AllowsFallbackToV1(t *testing.T) {
    r, err := NewRouter(EnvelopeSchema, WithRoutingPolicy(testRoutingPolicy{}))
    if err != nil {
        t.Fatalf("new router: %v", err)
    }
    called := false
    r.Register("T", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) HandlerResult {
        called = true
        return HandlerResult{ShouldDelete: true, Error: nil}
    })

    // Send v2 but only v1 is registered; policy should choose v1
    raw := []byte(`{"schemaVersion":"1.0","messageType":"T","messageVersion":"v2","message":{},"metadata":{}}`)
    rr := r.Route(context.Background(), raw)
    if !called || rr.HandlerResult.Error != nil || !rr.HandlerResult.ShouldDelete {
        t.Fatalf("routing policy fallback failed: %+v", rr)
    }
}

