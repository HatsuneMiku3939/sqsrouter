package sqsrouter

import (
	"context"
	"errors"
	policyfailure "github.com/hatsunemiku3939/sqsrouter/policy/failure"
	"github.com/hatsunemiku3939/sqsrouter/spec"
	"testing"
)

type testPolicy struct {
	lastKind spec.FailureKind
	lastErr  error
}

func (tp *testPolicy) Decide(ctx context.Context, kind spec.FailureKind, inner error, current spec.FailureResult) spec.FailureResult { //nolint:revive
	tp.lastKind = kind
	tp.lastErr = inner
	return current
}

func TestWithPolicy_SetsRouterPolicy(t *testing.T) {
	r, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("NewRouter err: %v", err)
	}
	if _, ok := r.failurePolicy.(policyfailure.ImmediateDeletePolicy); !ok {
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

	rr := spec.RoutedResult{HandlerResult: spec.HandlerResult{}}
	inner := errors.New("x")
	// simulate middleware failure path to invoke policy
	_ = r2.Route(context.Background(), []byte(`{}`)) // not strictly needed but ensure router constructed
	// Directly call Decide through interface to capture parameters
	_ = r2.failurePolicy.Decide(context.Background(), spec.FailMiddlewareError, inner, spec.FailureResult{ShouldDelete: rr.HandlerResult.ShouldDelete, Error: rr.HandlerResult.Error})
	if custom.lastKind != spec.FailMiddlewareError {
		t.Fatalf("expected custom policy to be invoked with kind=%v, got %v", spec.FailMiddlewareError, custom.lastKind)
	}
	if custom.lastErr != inner {
		t.Fatalf("expected custom policy to receive inner error")
	}
}
