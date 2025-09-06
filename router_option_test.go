package sqsrouter

import (
    "context"
    "errors"
    "testing"
)

type testPolicy struct {
    lastKind FailureKind
    lastErr  error
}

func (tp *testPolicy) Decide(ctx context.Context, kind FailureKind, inner error, current FailureResult) FailureResult { //nolint:revive
    tp.lastKind = kind
    tp.lastErr = inner
    return current
}

func TestWithPolicy_SetsRouterPolicy(t *testing.T) {
    r, err := NewRouter(EnvelopeSchema)
    if err != nil {
        t.Fatalf("NewRouter err: %v", err)
    }
    if _, ok := r.failurePolicy.(ImmediateDeletePolicy); !ok {
        t.Fatalf("expected default failure policy ImmediateDeletePolicy")
    }

    custom := &testPolicy{}
    r2, err := NewRouter(EnvelopeSchema, WithFailurePolicy(custom))
    if err != nil {
        t.Fatalf("NewRouter err: %v", err)
    }
    if r2.failurePolicy != custom {
        t.Fatalf("WithFailurePolicy did not set custom policy")
    }

    rr := RoutedResult{HandlerResult: HandlerResult{}}
    inner := errors.New("x")
    // simulate middleware failure path to invoke policy
    _ = r2.Route(context.Background(), []byte(`{}`)) // not strictly needed but ensure router constructed
    // Directly call Decide through interface to capture parameters
    _ = r2.failurePolicy.Decide(context.Background(), FailMiddlewareError, inner, FailureResult{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
    if custom.lastKind != FailMiddlewareError {
        t.Fatalf("expected custom policy to be invoked with kind=%v, got %v", FailMiddlewareError, custom.lastKind)
    }
    if custom.lastErr != inner {
        t.Fatalf("expected custom policy to receive inner error")
    }
}
