package sqsrouter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testMessageType    = "user.created"
	testMessageVersion = "1.0"
)

var (
	testUserCreatedSchema = `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"userId": { "type": "string" },
			"username": { "type": "string" }
		},
		"required": ["userId", "username"]
	}`

	testEnvelopeSchema = `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"schemaVersion": { "type": "string" },
			"messageType": { "type": "string" },
			"messageVersion": { "type": "string" },
			"message": { "type": "object" },
			"metadata": { "type": "object" }
		},
		"required": ["schemaVersion", "messageType", "messageVersion", "message", "metadata"]
	}`
)

// --- Test Helper Functions ---

func newTestRouter(t *testing.T) *Router {
	r, err := NewRouter(testEnvelopeSchema)
	require.NoError(t, err, "NewRouter should not fail with a valid schema")
	return r
}

func testSuccessHandler(_ context.Context, _, _ []byte) HandlerResult {
	return HandlerResult{ShouldDelete: true, Error: nil}
}

func testErrorHandler(_ context.Context, _, _ []byte) HandlerResult {
	return HandlerResult{ShouldDelete: true, Error: errors.New("handler failed")}
}

func testRetryHandler(_ context.Context, _, _ []byte) HandlerResult {
	return HandlerResult{ShouldDelete: false, Error: errors.New("transient error")}
}

func createTestMessage(t *testing.T, msgType, msgVersion, payload string) []byte {
	raw := fmt.Sprintf(`{
		"schemaVersion": "1.0",
		"messageType": "%s",
		"messageVersion": "%s",
		"message": %s,
		"metadata": {
			"timestamp": "2023-01-01T00:00:00Z",
			"source": "test",
			"messageId": "test-id-123"
		}
	}`, msgType, msgVersion, payload)
	return []byte(raw)
}

// --- Test Cases ---

func TestNewRouter(t *testing.T) {
	t.Run("should create router with valid schema", func(t *testing.T) {
		_, err := NewRouter(testEnvelopeSchema)
		assert.NoError(t, err)
	})

	t.Run("should fail with invalid schema", func(t *testing.T) {
		_, err := NewRouter(`{"type": "invalid"`)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid envelope schema")
	})
}

func TestRouter_Register(t *testing.T) {
	r := newTestRouter(t)
	r.Register(testMessageType, testMessageVersion, testSuccessHandler)

	key := makeKey(testMessageType, testMessageVersion)
	_, exists := r.handlers[key]
	assert.True(t, exists, "Handler should be registered")
}

func TestRouter_RegisterSchema(t *testing.T) {
	r := newTestRouter(t)

	t.Run("should register a valid schema", func(t *testing.T) {
		err := r.RegisterSchema(testMessageType, testMessageVersion, testUserCreatedSchema)
		assert.NoError(t, err)

		key := makeKey(testMessageType, testMessageVersion)
		_, exists := r.schemas[key]
		assert.True(t, exists, "Schema should be registered")
	})

	t.Run("should fail to register an invalid schema", func(t *testing.T) {
		err := r.RegisterSchema("test.type", "1.0", `{"type": "invalid"`)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid schema")
	})
}

func TestRouter_Route(t *testing.T) {
	t.Run("should route to correct handler on success", func(t *testing.T) {
		r := newTestRouter(t)
		r.Register(testMessageType, testMessageVersion, testSuccessHandler)

		payload := `{"userId": "123", "username": "test"}`
		msg := createTestMessage(t, testMessageType, testMessageVersion, payload)

		result := r.Route(context.Background(), msg)

		assert.NoError(t, result.HandlerResult.Error)
		assert.True(t, result.HandlerResult.ShouldDelete)
		assert.Equal(t, testMessageType, result.MessageType)
		assert.Equal(t, "test-id-123", result.MessageID)
	})

	t.Run("should return error from handler", func(t *testing.T) {
		r := newTestRouter(t)
		r.Register(testMessageType, testMessageVersion, testErrorHandler)

		payload := `{"userId": "123", "username": "test"}`
		msg := createTestMessage(t, testMessageType, testMessageVersion, payload)

		result := r.Route(context.Background(), msg)

		assert.Error(t, result.HandlerResult.Error)
		assert.Equal(t, "handler failed", result.HandlerResult.Error.Error())
		assert.True(t, result.HandlerResult.ShouldDelete)
	})

	t.Run("should handle retry logic from handler", func(t *testing.T) {
		r := newTestRouter(t)
		r.Register(testMessageType, testMessageVersion, testRetryHandler)

		payload := `{"userId": "123", "username": "test"}`
		msg := createTestMessage(t, testMessageType, testMessageVersion, payload)

		result := r.Route(context.Background(), msg)

		assert.Error(t, result.HandlerResult.Error)
		assert.Equal(t, "transient error", result.HandlerResult.Error.Error())
		assert.False(t, result.HandlerResult.ShouldDelete)
	})

	t.Run("should fail for unregistered handler", func(t *testing.T) {
		r := newTestRouter(t) // No handlers registered

		payload := `{"userId": "123", "username": "test"}`
		msg := createTestMessage(t, "unknown.type", "1.0", payload)

		result := r.Route(context.Background(), msg)

		assert.Error(t, result.HandlerResult.Error)
		assert.True(t, result.HandlerResult.ShouldDelete, "Should delete message with no handler")
		assert.Contains(t, result.HandlerResult.Error.Error(), "no handler registered")
	})

	t.Run("should fail on invalid envelope", func(t *testing.T) {
		r := newTestRouter(t)
		msg := []byte(`{"invalid": "message"}`)

		result := r.Route(context.Background(), msg)

		assert.Error(t, result.HandlerResult.Error)
		assert.True(t, result.HandlerResult.ShouldDelete, "Should delete malformed envelope")
		assert.Contains(t, result.HandlerResult.Error.Error(), "invalid envelope")
	})

	t.Run("should fail on malformed envelope json", func(t *testing.T) {
		r := newTestRouter(t)
		msg := []byte(`{"messageType": "test"`) // Invalid JSON

		result := r.Route(context.Background(), msg)

		assert.Error(t, result.HandlerResult.Error)
		assert.True(t, result.HandlerResult.ShouldDelete)
		assert.Contains(t, result.HandlerResult.Error.Error(), "invalid envelope")
	})

	t.Run("should fail on invalid message payload schema", func(t *testing.T) {
		r := newTestRouter(t)
		r.Register(testMessageType, testMessageVersion, testSuccessHandler)
		err := r.RegisterSchema(testMessageType, testMessageVersion, testUserCreatedSchema)
		require.NoError(t, err)

		// Payload is missing 'username'
		payload := `{"userId": "123"}`
		msg := createTestMessage(t, testMessageType, testMessageVersion, payload)

		result := r.Route(context.Background(), msg)

		assert.Error(t, result.HandlerResult.Error)
		assert.True(t, result.HandlerResult.ShouldDelete, "Should delete invalid payload")
		assert.Contains(t, result.HandlerResult.Error.Error(), "invalid message payload")
	})
}

func TestRouter_Concurrency(t *testing.T) {
	r := newTestRouter(t)
	r.Register(testMessageType, testMessageVersion, testSuccessHandler)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := `{"userId": "123", "username": "test"}`
			msg := createTestMessage(t, testMessageType, testMessageVersion, payload)
			result := r.Route(context.Background(), msg)
			assert.NoError(t, result.HandlerResult.Error)
		}()
	}

	// Concurrently register a new handler
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Register("another.type", "1.0", testSuccessHandler)
	}()

	wg.Wait()

	// Verify the new handler was registered
	key := makeKey("another.type", "1.0")
	_, exists := r.handlers[key]
	assert.True(t, exists, "New handler should be registered concurrently")
}
