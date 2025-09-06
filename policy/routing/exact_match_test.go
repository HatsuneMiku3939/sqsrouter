package routing

import (
    "context"
    "testing"

    "github.com/hatsunemiku3939/sqsrouter"
)

func TestExactMatchPolicy_Table(t *testing.T) {
    t.Parallel()
    p := ExactMatchPolicy{}
    ctx := context.Background()

    cases := []struct {
        name string
        env  sqsrouter.MessageEnvelope
        keys []sqsrouter.HandlerKey
        want sqsrouter.HandlerKey
    }{
        {
            name: "selects exact match",
            env:  sqsrouter.MessageEnvelope{MessageType: "A", MessageVersion: "v1"},
            keys: []sqsrouter.HandlerKey{"A:v0", "A:v1", "B:v1"},
            want: sqsrouter.HandlerKey("A:v1"),
        },
        {
            name: "returns empty when missing",
            env:  sqsrouter.MessageEnvelope{MessageType: "A", MessageVersion: "v9"},
            keys: []sqsrouter.HandlerKey{"A:v0", "B:v1"},
            want: sqsrouter.HandlerKey(""),
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
