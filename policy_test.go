package sqsrouter

import (
	"context"
	"errors"
	"testing"
)

func TestImmediateDeletePolicy_Decide(t *testing.T) {
	p := ImmediateDeletePolicy{}
	ctx := context.Background()
	st := &RouteState{}

	base := RoutedResult{
		MessageType:    "t",
		MessageVersion: "v",
		HandlerResult:  HandlerResult{ShouldDelete: false, Error: nil},
		MessageID:      "id",
		Timestamp:      "ts",
	}

	cases := []struct {
		name       string
		kind       FailureKind
		innerErr   error
		current    RoutedResult
		wantDelete bool
		wantHasErr bool
	}{
		{"FailNone_passthrough", FailNone, nil, base, false, false},
		{"FailEnvelopeSchema_delete", FailEnvelopeSchema, errors.New("schema"), base, true, true},
		{"FailEnvelopeParse_delete", FailEnvelopeParse, errors.New("parse"), base, true, true},
		{"FailPayloadSchema_delete", FailPayloadSchema, errors.New("payload"), base, true, true},
		{"FailNoHandler_delete", FailNoHandler, errors.New("nohandler"), base, true, true},
		{"FailHandlerPanic_delete", FailHandlerPanic, errors.New("panic"), base, true, true},
		{"FailMiddlewareError_retry_attach_err", FailMiddlewareError, errors.New("mw"), base, false, true},
		{"FailMiddlewareError_retry_preserve_existing_err", FailMiddlewareError, errors.New("ignored"), RoutedResult{
			MessageType:    "t",
			MessageVersion: "v",
			HandlerResult:  HandlerResult{ShouldDelete: false, Error: errors.New("already")},
			MessageID:      "id",
			Timestamp:      "ts",
		}, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Decide(ctx, st, tc.kind, tc.innerErr, tc.current)
			if got.HandlerResult.ShouldDelete != tc.wantDelete {
				t.Fatalf("ShouldDelete = %v, want %v", got.HandlerResult.ShouldDelete, tc.wantDelete)
			}
			if tc.wantHasErr && got.HandlerResult.Error == nil {
				t.Fatalf("expected error to be set, got nil")
			}
			if !tc.wantHasErr && got.HandlerResult.Error != nil {
				t.Fatalf("expected no error, got %v", got.HandlerResult.Error)
			}
		})
	}
}

type testPolicy struct {
	lastKind   FailureKind
	lastErr    error
	decideFunc func(ctx context.Context, st *RouteState, kind FailureKind, inner error, current RoutedResult) RoutedResult
}

func (tp *testPolicy) Decide(ctx context.Context, st *RouteState, kind FailureKind, inner error, current RoutedResult) RoutedResult {
	tp.lastKind = kind
	tp.lastErr = inner
	if tp.decideFunc != nil {
		return tp.decideFunc(ctx, st, kind, inner, current)
	}
	return current
}

func TestWithPolicy_SetsRouterPolicy(t *testing.T) {
	r, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("NewRouter err: %v", err)
	}
	if _, ok := r.policy.(ImmediateDeletePolicy); !ok {
		t.Fatalf("expected default policy ImmediateDeletePolicy")
	}

	custom := &testPolicy{}
	r2, err := NewRouter(EnvelopeSchema, WithPolicy(custom))
	if err != nil {
		t.Fatalf("NewRouter err: %v", err)
	}
	if r2.policy != custom {
		t.Fatalf("WithPolicy did not set custom policy")
	}

	rr := RoutedResult{HandlerResult: HandlerResult{}}
	inner := errors.New("x")
	_ = r2.policy.Decide(context.Background(), &RouteState{}, FailMiddlewareError, inner, rr)
	if custom.lastKind != FailMiddlewareError {
		t.Fatalf("expected custom policy to be invoked with kind=%v, got %v", FailMiddlewareError, custom.lastKind)
	}
	if custom.lastErr != inner {
		t.Fatalf("expected custom policy to receive inner error")
	}
}
func TestSQSRedrivePolicy_AllFailuresShouldNotDelete(t *testing.T) {
	p := SQSRedrivePolicy{}
	ctx := context.Background()
	st := &RouteState{}

	kinds := []FailureKind{
		FailEnvelopeSchema,
		FailEnvelopeParse,
		FailPayloadSchema,
		FailNoHandler,
		FailHandlerPanic,
		FailMiddlewareError,
	}

	for _, k := range kinds {
		inner := errors.New("inner")
		rr := RoutedResult{
			MessageType:    "t",
			MessageVersion: "v1",
			HandlerResult:  HandlerResult{ShouldDelete: true, Error: nil},
			MessageID:      "mid",
			Timestamp:      "ts",
		}
		got := p.Decide(ctx, st, k, inner, rr)
		if got.HandlerResult.ShouldDelete {
			t.Fatalf("kind %v: expected ShouldDelete=false, got true", k)
		}
		if got.HandlerResult.Error == nil {
			t.Fatalf("kind %v: expected Error to be set, got nil", k)
		}
	}
}

func TestSQSRedrivePolicy_FailNone_Unchanged(t *testing.T) {
	p := SQSRedrivePolicy{}
	ctx := context.Background()
	st := &RouteState{}

	orig := RoutedResult{
		MessageType:    "t",
		MessageVersion: "v1",
		HandlerResult:  HandlerResult{ShouldDelete: true, Error: nil},
		MessageID:      "mid",
		Timestamp:      "ts",
	}
	got := p.Decide(ctx, st, FailNone, nil, orig)
	if got.HandlerResult.ShouldDelete != orig.HandlerResult.ShouldDelete {
		t.Fatalf("expected ShouldDelete unchanged, got %v", got.HandlerResult.ShouldDelete)
	}
	if got.HandlerResult.Error != orig.HandlerResult.Error {
		t.Fatalf("expected Error unchanged")
	}
}

func TestSQSRedrivePolicy_ErrorAttachmentAndPreservation(t *testing.T) {
	p := SQSRedrivePolicy{}
	ctx := context.Background()
	st := &RouteState{}

	inner := errors.New("inner")
	rr := RoutedResult{HandlerResult: HandlerResult{ShouldDelete: true, Error: nil}}
	got := p.Decide(ctx, st, FailNoHandler, inner, rr)
	if got.HandlerResult.Error == nil {
		t.Fatalf("expected inner error attached")
	}
	if got.HandlerResult.ShouldDelete {
		t.Fatalf("expected ShouldDelete=false")
	}

	existing := errors.New("existing")
	rr2 := RoutedResult{HandlerResult: HandlerResult{ShouldDelete: true, Error: existing}}
	got2 := p.Decide(ctx, st, FailPayloadSchema, errors.New("ignored"), rr2)
	if got2.HandlerResult.Error != existing {
		t.Fatalf("expected existing error preserved, got %v", got2.HandlerResult.Error)
	}
	if got2.HandlerResult.ShouldDelete {
		t.Fatalf("expected ShouldDelete=false")
	}
}
