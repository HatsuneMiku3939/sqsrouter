package sqsrouter

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestMiddlewareOrderAndPrePost(t *testing.T) {
	router, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	var seq int32
	var seen []int32

	mw1 := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, s *RouteState) (RoutedResult, error) {
			seen = append(seen, atomic.AddInt32(&seq, 1))
			rr, err := next(ctx, s)
			seen = append(seen, atomic.AddInt32(&seq, 1))
			return rr, err
		}
	}
	mw2 := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, s *RouteState) (RoutedResult, error) {
			seen = append(seen, atomic.AddInt32(&seq, 1))
			rr, err := next(ctx, s)
			seen = append(seen, atomic.AddInt32(&seq, 1))
			return rr, err
		}
	}

	router.Use(mw1, mw2)

	router.Register("T", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) HandlerResult {
		return HandlerResult{ShouldDelete: true, Error: nil}
	})

	raw := []byte(`{"schemaVersion":"1.0","messageType":"T","messageVersion":"v1","message":{},"metadata":{}}`)
	_ = router.Route(context.Background(), raw)

	if len(seen) != 4 {
		t.Fatalf("want 4 calls, got %d", len(seen))
	}
	if !(seen[0] < seen[1] && seen[1] < seen[2] && seen[2] < seen[3]) {
		t.Fatalf("unexpected order: %v", seen)
	}
}

func TestMiddlewareErrorDoesNotForceDeleteByDefault(t *testing.T) {
	router, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	router.Register("T", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) HandlerResult {
		return HandlerResult{ShouldDelete: true, Error: nil}
	})

	errMW := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, s *RouteState) (RoutedResult, error) {
			rr, _ := next(ctx, s)
			return rr, errors.New("mw error")
		}
	}
	router.Use(errMW)

	raw := []byte(`{"schemaVersion":"1.0","messageType":"T","messageVersion":"v1","message":{},"metadata":{}}`)
	rr := router.Route(context.Background(), raw)

	if rr.HandlerResult.Error == nil {
		t.Fatalf("expected middleware error surfaced")
	}
	if !rr.HandlerResult.ShouldDelete {
		t.Fatalf("default policy should respect handler decision; got not delete")
	}
}

func TestMiddlewareErrorRespectsHandlerRetry(t *testing.T) {
	router, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	router.Register("T", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) HandlerResult {
		return HandlerResult{ShouldDelete: false, Error: errors.New("transient")}
	})

	errMW := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, s *RouteState) (RoutedResult, error) {
			rr, _ := next(ctx, s)
			return rr, errors.New("mw error")
		}
	}
	router.Use(errMW)

	raw := []byte(`{"schemaVersion":"1.0","messageType":"T","messageVersion":"v1","message":{},"metadata":{}}`)
	rr := router.Route(context.Background(), raw)

	if rr.HandlerResult.Error == nil {
		t.Fatalf("expected error present")
	}
	if rr.HandlerResult.ShouldDelete {
		t.Fatalf("default policy should not force delete on middleware error when handler asks retry")
	}
}

func TestMiddlewareRunsWhenNoHandlerRegistered(t *testing.T) {
	router, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	var ran int32
	mw := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, s *RouteState) (RoutedResult, error) {
			atomic.AddInt32(&ran, 1)
			rr, err := next(ctx, s)
			atomic.AddInt32(&ran, 1)
			return rr, err
		}
	}
	router.Use(mw)

	raw := []byte(`{"schemaVersion":"1.0","messageType":"Nope","messageVersion":"v1","message":{},"metadata":{}}`)
	_ = router.Route(context.Background(), raw)

	if ran == 0 {
		t.Fatalf("middleware did not run when no handler")
	}
}

func TestNoMiddlewareCompatibility(t *testing.T) {
	router, err := NewRouter(EnvelopeSchema)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	router.Register("T", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) HandlerResult {
		return HandlerResult{ShouldDelete: true, Error: nil}
	})

	raw := []byte(`{"schemaVersion":"1.0","messageType":"T","messageVersion":"v1","message":{},"metadata":{}}`)
	rr := router.Route(context.Background(), raw)

	if rr.MessageType != "T" || rr.MessageVersion != "v1" || !rr.HandlerResult.ShouldDelete || rr.HandlerResult.Error != nil {
		t.Fatalf("unexpected result without middleware: %+v", rr)
	}
}
