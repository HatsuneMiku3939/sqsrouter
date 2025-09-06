package failure

import "context"

// SQSRedrivePolicy always returns ShouldDelete=false for failures so SQS redrive handles retries/DLQ.
type SQSRedrivePolicy struct{}

// Decide implements the FailurePolicy interface for SQS redrive delegation.
func (p SQSRedrivePolicy) Decide(_ context.Context, kind Kind, inner error, current Result) Result {
	if kind == FailNone {
		return current
	}
	current.ShouldDelete = false
	if inner != nil && current.Error == nil {
		current.Error = inner
	}
	return current
}
