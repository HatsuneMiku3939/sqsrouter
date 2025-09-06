package failure

import (
    "context"
    "errors"
    "testing"
)

func TestSQSRedrivePolicy_NeverDeletesOnFailure(t *testing.T) {
    p := SQSRedrivePolicy{}
    kinds := []Kind{FailEnvelopeSchema, FailEnvelopeParse, FailPayloadSchema, FailNoHandler, FailHandlerPanic, FailMiddlewareError, FailHandlerError}
    for _, k := range kinds {
        inner := errors.New("x")
        cur := Result{ShouldDelete: true, Error: nil}
        got := p.Decide(context.Background(), k, inner, cur)
        if got.ShouldDelete {
            t.Fatalf("kind=%v: expected ShouldDelete=false", k)
        }
        if got.Error == nil || got.Error.Error() != inner.Error() {
            t.Fatalf("kind=%v: expected error to be inner", k)
        }
    }
}

func TestSQSRedrivePolicy_FailNonePassThrough(t *testing.T) {
    p := SQSRedrivePolicy{}
    cur := Result{ShouldDelete: true, Error: nil}
    got := p.Decide(context.Background(), FailNone, nil, cur)
    if got.ShouldDelete != cur.ShouldDelete || got.Error != nil {
        t.Fatalf("FailNone should pass through: %+v", got)
    }
}

