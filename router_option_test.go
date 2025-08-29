package sqsrouter

import (
    "context"
    "errors"
    "testing"

    "github.com/hatsunemiku3939/sqsrouter/policy"
)

type testPolicy struct {
    lastKind policy.FailureKind
    lastErr  error
}

func (tp *testPolicy) Decide(ctx context.Context, kind policy.FailureKind, inner error, current policy.Result) policy.Result { //nolint:revive
    tp.lastKind = kind
    tp.lastErr = inner
    return current
}

func TestWithPolicy_SetsRouterPolicy(t *testing.T) {
    r, err := NewRouter(EnvelopeSchema)
    if err != nil {
        t.Fatalf("NewRouter err: %v", err)
    }
    if _, ok := r.policy.(policy.ImmediateDeletePolicy); !ok {
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
    // simulate middleware failure path to invoke policy
    _ = r2.Route(context.Background(), []byte(`{}`)) // not strictly needed but ensure router constructed
    // Directly call Decide through interface to capture parameters
    _ = r2.policy.Decide(context.Background(), policy.FailMiddlewareError, inner, policy.Result{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
    if custom.lastKind != policy.FailMiddlewareError {
        t.Fatalf("expected custom policy to be invoked with kind=%v, got %v", policy.FailMiddlewareError, custom.lastKind)
    }
    if custom.lastErr != inner {
        t.Fatalf("expected custom policy to receive inner error")
    }
}

