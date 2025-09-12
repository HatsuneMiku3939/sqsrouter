package failure

import (
	"context"
	"errors"
	"testing"

	"github.com/hatsunemiku3939/sqsrouter/spec"
)

func TestImmediateDeletePolicyDecide(t *testing.T) {
	p := ImmediateDeletePolicy{}
	ctx := context.Background()

	base := spec.FailureResult{ShouldDelete: false, Error: nil}

	cases := []struct {
		name       string
		kind       spec.FailureKind
		innerErr   error
		current    spec.FailureResult
		wantDelete bool
		wantHasErr bool
	}{
		{"FailNone_passthrough", spec.FailNone, nil, base, false, false},
		{"FailEnvelopeSchema_delete", spec.FailEnvelopeSchema, errors.New("schema"), base, true, true},
		{"FailEnvelopeParse_delete", spec.FailEnvelopeParse, errors.New("parse"), base, true, true},
		{"FailPayloadSchema_delete", spec.FailPayloadSchema, errors.New("payload"), base, true, true},
		{"FailNoHandler_delete", spec.FailNoHandler, errors.New("nohandler"), base, true, true},
		{"FailHandlerError_respect_handler", spec.FailHandlerError, errors.New("handler"), base, false, true},
		{"FailHandlerPanic_delete", spec.FailHandlerPanic, errors.New("panic"), base, true, true},
		{"FailMiddlewareError_retry_attach_err", spec.FailMiddlewareError, errors.New("mw"), base, false, true},
		{"FailMiddlewareError_retry_preserve_existing_err", spec.FailMiddlewareError, errors.New("ignored"), spec.FailureResult{ShouldDelete: false, Error: errors.New("already")}, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Decide(ctx, tc.kind, tc.innerErr, tc.current)
			if got.ShouldDelete != tc.wantDelete {
				t.Fatalf("ShouldDelete = %v, want %v", got.ShouldDelete, tc.wantDelete)
			}
			if tc.wantHasErr && got.Error == nil {
				t.Fatalf("expected error to be set, got nil")
			}
			if !tc.wantHasErr && got.Error != nil {
				t.Fatalf("expected no error, got %v", got.Error)
			}
		})
	}
}
