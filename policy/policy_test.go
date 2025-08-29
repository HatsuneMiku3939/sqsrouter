package policy

import (
    "context"
    "errors"
    "testing"
)

func TestImmediateDeletePolicy_Decide(t *testing.T) {
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

func TestSQSRedrivePolicy_AllFailuresShouldNotDelete(t *testing.T) {
    p := SQSRedrivePolicy{}
    ctx := context.Background()

    kinds := []FailureKind{
        FailEnvelopeSchema,
        FailEnvelopeParse,
        FailPayloadSchema,
        FailNoHandler,
        FailHandlerError,
        FailHandlerPanic,
        FailMiddlewareError,
    }

    for _, k := range kinds {
        inner := errors.New("inner")
        curr := Result{ShouldDelete: true, Error: nil}
        got := p.Decide(ctx, k, inner, curr)
        if got.ShouldDelete {
            t.Fatalf("kind %v: expected ShouldDelete=false, got true", k)
        }
        if got.Error == nil {
            t.Fatalf("kind %v: expected Error to be set, got nil", k)
        }
    }
}

func TestSQSRedrivePolicy_FailNone_Unchanged(t *testing.T) {
    p := SQSRedrivePolicy{}
    ctx := context.Background()

    orig := Result{ShouldDelete: true, Error: nil}
    got := p.Decide(ctx, FailNone, nil, orig)
    if got.ShouldDelete != orig.ShouldDelete {
        t.Fatalf("expected ShouldDelete unchanged, got %v", got.ShouldDelete)
    }
    if got.Error != orig.Error {
        t.Fatalf("expected Error unchanged")
    }
}

func TestSQSRedrivePolicy_ErrorAttachmentAndPreservation(t *testing.T) {
    p := SQSRedrivePolicy{}
    ctx := context.Background()

    inner := errors.New("inner")
    rr := Result{ShouldDelete: true, Error: nil}
    got := p.Decide(ctx, FailNoHandler, inner, rr)
    if got.Error == nil {
        t.Fatalf("expected inner error attached")
    }
    if got.ShouldDelete {
        t.Fatalf("expected ShouldDelete=false")
    }

    existing := errors.New("existing")
    rr2 := Result{ShouldDelete: true, Error: existing}
    got2 := p.Decide(ctx, FailPayloadSchema, errors.New("ignored"), rr2)
    if got2.Error != existing {
        t.Fatalf("expected existing error preserved, got %v", got2.Error)
    }
    if got2.ShouldDelete {
        t.Fatalf("expected ShouldDelete=false")
    }
}

