package consumer

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sqsrouter "github.com/hatsunemiku3939/sqsrouter"
	"github.com/hatsunemiku3939/sqsrouter/spec"
)

func TestFIFOConsumer_SequentialSuccessDeletesBoth(t *testing.T) {
	queueURL := "test-queue"
	mockClient := new(MockSQSClient)

	router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
	require.NoError(t, err)

	// Two message types to differentiate handlers
	typeA, ver := "typeA", "1.0"
	typeB := "typeB"
	router.Register(typeA, ver, func(ctx context.Context, msg []byte) spec.HandlerResult {
		return spec.HandlerResult{ShouldDelete: true}
	})
	router.Register(typeB, ver, func(ctx context.Context, msg []byte) spec.HandlerResult {
		return spec.HandlerResult{ShouldDelete: true}
	})

	c := NewFIFOConsumer(mockClient, queueURL, router)

	// Prepare two messages in one batch
	bodyA := fmt.Sprintf(`{"schemaVersion":"1.0","messageType":"%s","messageVersion":"%s","message":{},"metadata":{"messageId":"a"}}`, typeA, ver)
	bodyB := fmt.Sprintf(`{"schemaVersion":"1.0","messageType":"%s","messageVersion":"%s","message":{},"metadata":{"messageId":"b"}}`, typeB, ver)
	r1 := "receipt-1"
	r2 := "receipt-2"
	msgA := sqstypes.Message{Body: &bodyA, ReceiptHandle: &r1}
	msgB := sqstypes.Message{Body: &bodyB, ReceiptHandle: &r2}

	// Expect one ReceiveMessage returning both messages, then cancel
	ctx, cancel := context.WithCancel(context.Background())
	mockClient.
		On("ReceiveMessage", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { cancel() }).
		Return(&sqs.ReceiveMessageOutput{Messages: []sqstypes.Message{msgA, msgB}}, nil).
		Once()

	// Expect DeleteMessage called for r1 then r2
	call1 := mockClient.On("DeleteMessage", mock.Anything, mock.MatchedBy(func(in *sqs.DeleteMessageInput) bool {
		return in != nil && in.ReceiptHandle != nil && *in.ReceiptHandle == r1
	})).Return(&sqs.DeleteMessageOutput{}, nil).Once()
	call2 := mockClient.On("DeleteMessage", mock.Anything, mock.MatchedBy(func(in *sqs.DeleteMessageInput) bool {
		return in != nil && in.ReceiptHandle != nil && *in.ReceiptHandle == r2
	})).Return(&sqs.DeleteMessageOutput{}, nil).Once()
	mock.InOrder(call1, call2)

	c.Start(ctx)

	mockClient.AssertExpectations(t)
}

func TestFIFOConsumer_FailFastStopsBatch(t *testing.T) {
	queueURL := "test-queue"
	mockClient := new(MockSQSClient)

	router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
	require.NoError(t, err)

	typeA, ver := "typeA", "1.0"
	typeB := "typeB"

	// First handler fails and ShouldDelete=false
	router.Register(typeA, ver, func(ctx context.Context, msg []byte) spec.HandlerResult {
		return spec.HandlerResult{ShouldDelete: false, Error: fmt.Errorf("transient")}
	})

	// Second handler increments a counter if ever called
	var bCount int32
	router.Register(typeB, ver, func(ctx context.Context, msg []byte) spec.HandlerResult {
		atomic.AddInt32(&bCount, 1)
		return spec.HandlerResult{ShouldDelete: true}
	})

	c := NewFIFOConsumer(mockClient, queueURL, router)

	bodyA := fmt.Sprintf(`{"schemaVersion":"1.0","messageType":"%s","messageVersion":"%s","message":{},"metadata":{"messageId":"a"}}`, typeA, ver)
	bodyB := fmt.Sprintf(`{"schemaVersion":"1.0","messageType":"%s","messageVersion":"%s","message":{},"metadata":{"messageId":"b"}}`, typeB, ver)
	r1 := "receipt-1"
	r2 := "receipt-2"
	msgA := sqstypes.Message{Body: &bodyA, ReceiptHandle: &r1}
	msgB := sqstypes.Message{Body: &bodyB, ReceiptHandle: &r2}

	// One poll then cancel
	ctx, cancel := context.WithCancel(context.Background())
	mockClient.
		On("ReceiveMessage", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { cancel() }).
		Return(&sqs.ReceiveMessageOutput{Messages: []sqstypes.Message{msgA, msgB}}, nil).
		Once()

	// Expect no DeleteMessage calls because first message returns ShouldDelete=false
	c.Start(ctx)

	mockClient.AssertExpectations(t)
	assert.Equal(t, int32(0), atomic.LoadInt32(&bCount), "second handler should not run on fail-fast")
}
