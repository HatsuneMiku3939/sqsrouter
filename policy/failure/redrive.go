package failure

import (
	"context"

	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// SQSRedrivePolicy always returns ShouldDelete=false for failures so SQS redrive handles retries/DLQ.
type SQSRedrivePolicy struct{}

// Decide implements the FailurePolicy interface for SQS redrive delegation.
func (p SQSRedrivePolicy) Decide(_ context.Context, kind spec.FailureKind, inner error, current spec.FailureResult) spec.FailureResult {
	if kind == spec.FailNone {
		return current
	}
	current.ShouldDelete = false
	if inner != nil && current.Error == nil {
		current.Error = inner
	}
	return current
}
