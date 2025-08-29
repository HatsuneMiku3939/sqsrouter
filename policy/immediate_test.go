package policy

import (
	"context"
	"errors"
	"testing"
)

func TestImmediateDeletePolicyDecide(t *testing.T) {
	p := ImmediateDeletePolicy{}
	ctx := context.Background()

	base := Result{ShouldDelete: false, Error: nil}

	cases := []struct {
		name       string
		kind       FailureKind
		innerErr   error
		current    Result
		wantDelete bool
		wantHasErr bool
	}{
		{"FailNone_passthrough", FailNone, nil, base, false, false},
		{"FailEnvelopeSchema_delete", FailEnvelopeSchema, errors.New("schema"), base, true, true},
		{"FailEnvelopeParse_delete", FailEnvelopeParse, errors.New("parse"), base, true, true},
		{"FailPayloadSchema_delete", FailPayloadSchema, errors.New("payload"), base, true, true},
		{"FailNoHandler_delete", FailNoHandler, errors.New("nohandler"), base, true, true},
		{"FailHandlerError_respect_handler", FailHandlerError, errors.New("handler"), base, false, true},
		{"FailHandlerPanic_delete", FailHandlerPanic, errors.New("panic"), base, true, true},
		{"FailMiddlewareError_retry_attach_err", FailMiddlewareError, errors.New("mw"), base, false, true},
		{"FailMiddlewareError_retry_preserve_existing_err", FailMiddlewareError, errors.New("ignored"), Result{ShouldDelete: false, Error: errors.New("already")}, false, true},
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
