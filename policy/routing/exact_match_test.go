package routing

import (
	"context"
	"testing"

	stypes "github.com/hatsunemiku3939/sqsrouter/types"
)

func TestExactMatchPolicy_Table(t *testing.T) {
	t.Parallel()
	p := ExactMatchPolicy{}
	ctx := context.Background()

	cases := []struct {
		name string
		env  stypes.MessageEnvelope
		keys []stypes.HandlerKey
		want stypes.HandlerKey
	}{
		{
			name: "selects exact match",
			env:  stypes.MessageEnvelope{MessageType: "A", MessageVersion: "v1"},
			keys: []stypes.HandlerKey{"A:v0", "A:v1", "B:v1"},
			want: stypes.HandlerKey("A:v1"),
		},
		{
			name: "returns empty when missing",
			env:  stypes.MessageEnvelope{MessageType: "A", MessageVersion: "v9"},
			keys: []stypes.HandlerKey{"A:v0", "B:v1"},
			want: stypes.HandlerKey(""),
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
