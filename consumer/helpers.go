package consumer

import (
	"strconv"
	"time"

	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/hatsunemiku3939/sqsrouter/spec"
)

// buildMessageContext constructs a spec.MessageContext from an SQS message.
func buildMessageContext(m *sqstypes.Message) *spec.MessageContext {
	if m == nil {
		return &spec.MessageContext{}
	}
	mc := &spec.MessageContext{CustomAttributes: map[string]sqstypes.MessageAttributeValue{}}
	// Copy message attributes directly (safe to range over nil map)
	for k, v := range m.MessageAttributes {
		mc.CustomAttributes[k] = v
	}
	// Parse system attributes (map lookup on nil map is safe)
	if v, ok := m.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			mc.ReceiveCount = n
		}
	}
	if v, ok := m.Attributes[string(sqstypes.MessageSystemAttributeNameSentTimestamp)]; ok {
		if ts, err := parseEpochMillis(v); err == nil {
			mc.SentTimestamp = ts
		}
	}
	if v, ok := m.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateFirstReceiveTimestamp)]; ok {
		if ts, err := parseEpochMillis(v); err == nil {
			mc.FirstReceiveTimestamp = ts
		}
	}
	if v, ok := m.Attributes[string(sqstypes.MessageSystemAttributeNameMessageGroupId)]; ok {
		mc.MessageGroupID = v
	}
	if v, ok := m.Attributes[string(sqstypes.MessageSystemAttributeNameMessageDeduplicationId)]; ok {
		mc.MessageDeduplicationID = v
	}
	return mc
}

// parseEpochMillis converts a string epoch millis to time.Time.
func parseEpochMillis(s string) (time.Time, error) {
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ms*int64(time.Millisecond)), nil
}
