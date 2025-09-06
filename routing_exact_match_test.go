package sqsrouter

import (
    "context"
    "testing"
)

func TestExactMatchPolicy_Table(t *testing.T) {
    t.Parallel()
    p := ExactMatchPolicy{}
    ctx := context.Background()

    cases := []struct {
        name string
        env  MessageEnvelope
        keys []HandlerKey
        want HandlerKey
    }{
        {
            name: "selects exact match",
            env:  MessageEnvelope{MessageType: "A", MessageVersion: "v1"},
            keys: []HandlerKey{"A:v0", "A:v1", "B:v1"},
            want: HandlerKey("A:v1"),
        },
        {
            name: "returns empty when missing",
            env:  MessageEnvelope{MessageType: "A", MessageVersion: "v9"},
            keys: []HandlerKey{"A:v0", "B:v1"},
            want: HandlerKey(""),
        },
    }

    for _, tc := range cases {
        tc := tc
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            got := p.Decide(ctx, &tc.env, tc.keys)
            if got != tc.want {
                t.Fatalf("want %q, got %q", tc.want, got)
            }
        })
    }
}
