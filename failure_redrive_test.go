package sqsrouter

import (
    "context"
    "errors"
    "testing"
)

func TestSQSRedrivePolicyAllFailuresShouldNotDelete(t *testing.T) {
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
        curr := FailureResult{ShouldDelete: true, Error: nil}
        got := p.Decide(ctx, k, inner, curr)
        if got.ShouldDelete {
            t.Fatalf("kind %v: expected ShouldDelete=false, got true", k)
        }
        if got.Error == nil {
            t.Fatalf("kind %v: expected Error to be set, got nil", k)
        }
    }
}

func TestSQSRedrivePolicyFailNoneUnchanged(t *testing.T) {
    p := SQSRedrivePolicy{}
    ctx := context.Background()

    orig := FailureResult{ShouldDelete: true, Error: nil}
    got := p.Decide(ctx, FailNone, nil, orig)
    if got.ShouldDelete != orig.ShouldDelete {
        t.Fatalf("expected ShouldDelete unchanged, got %v", got.ShouldDelete)
    }
    if got.Error != orig.Error {
        t.Fatalf("expected Error unchanged")
    }
}

func TestSQSRedrivePolicyErrorAttachmentAndPreservation(t *testing.T) {
    p := SQSRedrivePolicy{}
    ctx := context.Background()

    inner := errors.New("inner")
    rr := FailureResult{ShouldDelete: true, Error: nil}
    got := p.Decide(ctx, FailNoHandler, inner, rr)
    if got.Error == nil {
        t.Fatalf("expected inner error attached")
    }
    if got.ShouldDelete {
        t.Fatalf("expected ShouldDelete=false")
    }

    existing := errors.New("existing")
    rr2 := FailureResult{ShouldDelete: true, Error: existing}
    got2 := p.Decide(ctx, FailPayloadSchema, errors.New("ignored"), rr2)
    if got2.Error != existing {
        t.Fatalf("expected existing error preserved, got %v", got2.Error)
    }
    if got2.ShouldDelete {
        t.Fatalf("expected ShouldDelete=false")
    }
}
