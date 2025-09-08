package sqsrouter

import (
	policyfailure "github.com/hatsunemiku3939/sqsrouter/policy/failure"
	policyrouting "github.com/hatsunemiku3939/sqsrouter/policy/routing"
)

// Re-export built-in policies for backward compatibility at root package.
type (
	ExactMatchPolicy      = policyrouting.ExactMatchPolicy
	ImmediateDeletePolicy = policyfailure.ImmediateDeletePolicy
	SQSRedrivePolicy      = policyfailure.SQSRedrivePolicy
)
