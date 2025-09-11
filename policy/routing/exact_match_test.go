package routing

import (
    "context"
    "testing"

    "github.com/hatsunemiku3939/sqsrouter/spec"
)

func TestExactMatchPolicy_Table(t *testing.T) {
    t.Parallel()
    p := ExactMatchPolicy{}
    ctx := context.Background()

    cases := []struct {
        name string
        env  spec.MessageEnvelope
        keys []spec.HandlerKey
        want spec.HandlerKey
    }{
        {
            name: "selects exact match",
            env:  spec.MessageEnvelope{MessageType: "A", MessageVersion: "v1"},
            keys: []spec.HandlerKey{"A:v0", "A:v1", "B:v1"},
            want: spec.HandlerKey("A:v1"),
        },
        {
            name: "returns empty when missing",
            env:  spec.MessageEnvelope{MessageType: "A", MessageVersion: "v9"},
            keys: []spec.HandlerKey{"A:v0", "B:v1"},
            want: spec.HandlerKey(""),
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

