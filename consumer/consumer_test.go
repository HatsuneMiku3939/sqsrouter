package consumer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sqsrouter "github.com/hatsunemiku3939/sqsrouter"
)

// --- Mock SQSClient ---

type MockSQSClient struct{ mock.Mock }

func (m *MockSQSClient) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sqs.ReceiveMessageOutput), args.Error(1)
}

func (m *MockSQSClient) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sqs.DeleteMessageOutput), args.Error(1)
}

func createSQSMessage(body, receiptHandle string) types.Message {
	return types.Message{Body: &body, ReceiptHandle: &receiptHandle}
}

func TestNewConsumer(t *testing.T) {
	mockClient := new(MockSQSClient)
	router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
	require.NoError(t, err)
	c := NewConsumer(mockClient, "test-queue-url", router)

	assert.NotNil(t, c)
	assert.Equal(t, "test-queue-url", c.queueURL)
	assert.Equal(t, mockClient, c.client)
	assert.Equal(t, router, c.router)
}

func TestConsumer_processMessage(t *testing.T) {
	queueURL := "test-queue"

	tests := []struct {
		name                 string
		handler              sqsrouter.MessageHandler
		shouldDelete         bool
		expectDeleteCall     bool
		deleteShouldFail     bool
		expectedDeleteErrMsg string
	}{
		{
			name: "success, should delete",
			handler: func(ctx context.Context, msg []byte, meta []byte) sqsrouter.HandlerResult {
				return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
			},
			shouldDelete:     true,
			expectDeleteCall: true,
		},
		{
			name: "handler error, but should delete",
			handler: func(ctx context.Context, msg []byte, meta []byte) sqsrouter.HandlerResult {
				return sqsrouter.HandlerResult{ShouldDelete: true, Error: errors.New("permanent failure")}
			},
			shouldDelete:     true,
			expectDeleteCall: true,
		},
		{
			name: "handler error, should not delete (retry)",
			handler: func(ctx context.Context, msg []byte, meta []byte) sqsrouter.HandlerResult {
				return sqsrouter.HandlerResult{ShouldDelete: false, Error: errors.New("transient error")}
			},
			shouldDelete:     false,
			expectDeleteCall: false,
		},
		{
			name: "success, but delete fails",
			handler: func(ctx context.Context, msg []byte, meta []byte) sqsrouter.HandlerResult {
				return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
			},
			shouldDelete:         true,
			expectDeleteCall:     true,
			deleteShouldFail:     true,
			expectedDeleteErrMsg: "failed to delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(MockSQSClient)
			router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
			require.NoError(t, err)

			msgType, msgVersion := "test.event", "1.0"
			router.Register(msgType, msgVersion, tt.handler)

			c := NewConsumer(mockClient, queueURL, router)

			msgBody := fmt.Sprintf(`{
                "schemaVersion": "1.0", "messageType": "%s", "messageVersion": "%s",
                "message": {}, "metadata": {"messageId": "msg-1"}
            }`, msgType, msgVersion)
			sqsMsg := createSQSMessage(msgBody, "receipt-1")

			if tt.expectDeleteCall {
				deleteCall := mockClient.On("DeleteMessage", mock.Anything, mock.Anything)
				if tt.deleteShouldFail {
					deleteCall.Return(nil, errors.New(tt.expectedDeleteErrMsg))
				} else {
					deleteCall.Return(&sqs.DeleteMessageOutput{}, nil)
				}
			}

			c.processMessage(context.Background(), &sqsMsg)

			mockClient.AssertExpectations(t)
			if !tt.expectDeleteCall {
				mockClient.AssertNotCalled(t, "DeleteMessage")
			}
		})
	}

	t.Run("should not process message with nil body", func(t *testing.T) {
		mockClient := new(MockSQSClient)
		router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
		require.NoError(t, err)
		c := NewConsumer(mockClient, queueURL, router)

		sqsMsg := types.Message{Body: nil, ReceiptHandle: new(string)}
		c.processMessage(context.Background(), &sqsMsg)
		// No client calls should be made
		mockClient.AssertNotCalled(t, "DeleteMessage")
	})
}

func TestConsumer_Start(t *testing.T) {
	queueURL := "test-queue"
	mockClient := new(MockSQSClient)
	router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
	require.NoError(t, err)

	// Setup a simple success handler
	msgType, msgVersion := "test.event", "1.0"
	router.Register(msgType, msgVersion, func(ctx context.Context, msg []byte, meta []byte) sqsrouter.HandlerResult {
		return sqsrouter.HandlerResult{ShouldDelete: true}
	})

	c := NewConsumer(mockClient, queueURL, router)

	t.Run("receives and deletes message successfully", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		msgBody := fmt.Sprintf(`{"schemaVersion":"1.0","messageType":"%s","messageVersion":"%s","message":{},"metadata":{"messageId":"msg-1"}}`, msgType, msgVersion)
		sqsMsg := createSQSMessage(msgBody, "receipt-1")
		receiveOutput := &sqs.ReceiveMessageOutput{Messages: []types.Message{sqsMsg}}

		// Expect ReceiveMessage, then stop the consumer
		mockClient.On("ReceiveMessage", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			cancel() // Stop the consumer after the first poll
		}).Return(receiveOutput, nil).Once()

		// Expect DeleteMessage to be called for the received message
		mockClient.On("DeleteMessage", mock.Anything, mock.Anything).Return(&sqs.DeleteMessageOutput{}, nil).Once()

		c.Start(ctx)

		mockClient.AssertExpectations(t)
	})

	t.Run("handles receive message error gracefully", func(t *testing.T) {
		mockClient := new(MockSQSClient) // Reset mock for this test
		c := NewConsumer(mockClient, queueURL, router)
		// The consumer sleeps for 2s on error, so context must be longer.
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		defer cancel()

		// Expect ReceiveMessage to be called and fail, then consumer will sleep.
		// After sleep, context will be checked again and it will be expired.
		mockClient.On("ReceiveMessage", mock.Anything, mock.Anything).Return(nil, errors.New("SQS error")).Run(func(args mock.Arguments) {
			// Cancel the context after the first error to ensure the loop terminates.
			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()
		}).Once()

		c.Start(ctx)

		mockClient.AssertExpectations(t)
	})
}
