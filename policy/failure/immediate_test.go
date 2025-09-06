package failure

import (
    "context"
    "errors"
    "testing"
)

func TestImmediateDeletePolicy_StructuralFailuresForceDelete(t *testing.T) {
    p := ImmediateDeletePolicy{}
    kinds := []Kind{FailEnvelopeSchema, FailEnvelopeParse, FailPayloadSchema, FailNoHandler, FailHandlerPanic}
    for _, k := range kinds {
        inner := errors.New("boom")
        cur := Result{ShouldDelete: false, Error: nil}
        got := p.Decide(context.Background(), k, inner, cur)
        if !got.ShouldDelete {
            t.Fatalf("kind=%v: expected ShouldDelete=true", k)
        }
        if got.Error == nil || got.Error.Error() != inner.Error() {
            t.Fatalf("kind=%v: expected error to be inner", k)
        }
    }
}

func TestImmediateDeletePolicy_HandlerAndMiddlewarePreserveDecision(t *testing.T) {
    p := ImmediateDeletePolicy{}
    kinds := []Kind{FailMiddlewareError, FailHandlerError}
    inner := errors.New("handler fail")

    // current delete=true should remain true
    cur := Result{ShouldDelete: true, Error: nil}
    for _, k := range kinds {
        got := p.Decide(context.Background(), k, inner, cur)
        if !got.ShouldDelete {
            t.Fatalf("kind=%v: expected ShouldDelete to remain true", k)
        }
        if got.Error == nil || got.Error.Error() != inner.Error() {
            t.Fatalf("kind=%v: expected error to be inner", k)
        }
    }

    // current delete=false should remain false
    cur = Result{ShouldDelete: false, Error: nil}
    for _, k := range kinds {
        got := p.Decide(context.Background(), k, inner, cur)
        if got.ShouldDelete {
            t.Fatalf("kind=%v: expected ShouldDelete to remain false", k)
        }
        if got.Error == nil || got.Error.Error() != inner.Error() {
            t.Fatalf("kind=%v: expected error to be inner", k)
        }
    }
}

func TestImmediateDeletePolicy_FailNonePassThrough(t *testing.T) {
    p := ImmediateDeletePolicy{}
    cur := Result{ShouldDelete: true, Error: nil}
    got := p.Decide(context.Background(), FailNone, nil, cur)
    if got.ShouldDelete != cur.ShouldDelete || got.Error != nil {
        t.Fatalf("FailNone should pass through: %+v", got)
    }
}

