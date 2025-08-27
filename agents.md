# Middleware

Router supports a middleware chain around the Route pipeline.

API:
- type HandlerFunc func(ctx context.Context, state *RouteState) (RoutedResult, error)
- type Middleware func(next HandlerFunc) HandlerFunc
- func (r *Router) Use(mw ...Middleware)
- func (r *Router) WithFailFast(v bool)

Behavior:
- Middlewares run in registration order and wrap the core routing steps.
- Middlewares can read RouteState and modify the returned RoutedResult.
- Middlewares execute even if no handler is registered.
- Fail-fast is optional; when enabled, middleware errors map to ShouldDelete=true.

# agents.md — sqsrouter guide

This document is a practical guide to help you understand and use the sqsrouter codebase quickly. It consolidates architecture, usage, and operational tips so an AI agent or automation can reliably process SQS messages. Let’s go♪

- Repository: github.com/HatsuneMiku3939/sqsrouter
- Core components: Consumer(SQS polling/deletion), Router(validation/dispatch), Handler(user-defined logic)

## 1) Overview
- Purpose: Route JSON messages pulled from AWS SQS to handlers by message type/version and validate them with JSON Schemas, providing a consistent processing pipeline.
- Benefits
  - Standardized routing rules and schema validation
  - Clear failure/retry/delete policy
  - Built-in patterns for concurrency, timeouts, and graceful shutdown

## 2) Architecture
Pipeline flow:
- Consumer: Receives message batches via SQS Long Polling → processes each message in its own goroutine
- Router: Validates the envelope → finds the handler → optionally validates payload against a registered schema → runs the handler → returns a RoutedResult
- Handler: Executes business logic and returns a HandlerResult (delete decision and error)

Key types:
- Exported public types are defined in types.go
- Consumer: NewConsumer(client, queueURL, router).Start(ctx)
- Router: NewRouter(envelopeSchema).Register(type, version, handler).RegisterSchema(type, version, schema).Route(ctx, raw)

## 3) Message format and schema
The envelope is the basis for routing and validation.

```json
{
  "schemaVersion": "1.0",
  "messageType": "UserCreated",
  "messageVersion": "v1",
  "message": { "userId": "123", "name": "Alice" },
  "metadata": { "timestamp": "2024-01-01T00:00:00Z", "source": "svcA", "messageId": "uuid-..." }
}
```

Envelope schema (excerpt from code):

```go
var EnvelopeSchema = `{
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
```

- metadata.timestamp/messageId are great for log correlation.
- Even if metadata parsing fails, the message processing continues with a warning log.

## 4) Router usage
```go
// Create router with envelope schema
router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
if err != nil {
    panic(err) // (Do not panic in production; handle errors properly.)
}

// Register handler
router.Register("UserCreated", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) sqsrouter.HandlerResult {
    // Parse msgJSON to struct and process
    // return ShouldDelete=true for permanent outcomes; false to retry
    return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
})

// (Optional) Register payload schema for validation
router.RegisterSchema("UserCreated", "v1", `{
  "$schema":"http://json-schema.org/draft-07/schema#",
  "type":"object",
  "properties":{ "userId":{"type":"string"}, "name":{"type":"string"} },
  "required":["userId","name"]
}`)
```

Route flow:
1) Validate envelope (invalid structure → permanent failure → delete)
2) Unmarshal envelope
3) Lookup handler (missing handler → permanent failure → delete)
4) Validate payload schema if registered (invalid → delete)
5) Parse metadata (failure only warns)
6) Run handler → collect HandlerResult

Contract:
- HandlerResult.ShouldDelete
  - true: consider processing complete (success or permanent failure) → delete
  - false: treat as transient error → do not delete (will be retried after visibility timeout)
- HandlerResult.Error
  - nil: success
  - non-nil: failure (logged; delete or retry per ShouldDelete)

## 5) Consumer usage
Key constants:
- maxMessages=5, waitTimeSeconds=10 (Long Polling), processingTimeout=30s, deleteTimeout=5s, retrySleep=2s

```go
// Build AWS SQS client (aws-sdk-go-v2) and create Consumer
client := sqs.NewFromConfig(cfg)
consumer := sqsrouter.NewConsumer(client, "https://sqs.{region}.amazonaws.com/{account}/{queue}", router)

// Start polling (blocking until ctx canceled)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
consumer.Start(ctx)
```

Processing/deletion logic:
- If HandlerResult.ShouldDelete is true, Consumer calls DeleteMessage
- If false, it leaves the message for retry (after visibility timeout)

Graceful shutdown:
- Start loop stops polling on ctx cancellation
- Waits for in-flight goroutines to finish

## 6) Quick start example
Use example/basic for the fastest start.

Path: example/basic/main.go

```go
// Pseudocode summary (refer to the real example in the repo)
router, _ := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
router.Register("UserCreated", "v1", userCreatedHandler)
router.RegisterSchema("UserCreated", "v1", userCreatedSchemaJSON)

client := sqs.NewFromConfig(cfg)
consumer := sqsrouter.NewConsumer(client, queueURL, router)
consumer.Start(ctx)
```

For local e2e testing, see test/docker-compose.yaml and test/e2e.sh. You can use LocalStack to emulate SQS.

## 7) Operations guide
Concurrency/timeouts:
- maxMessages: messages per poll; balance throughput vs memory
- waitTimeSeconds: long polling duration; reduces cost/empty responses
- processingTimeout: cap for handler processing time; keep below container termination grace period
- deleteTimeout: timeout for DeleteMessage API call
- Consider SQS Visibility Timeout vs processingTimeout when configuring

Failure handling:
- Permanent failures (e.g., invalid payload schema, missing handler): ShouldDelete=true → delete. Configure a DLQ and monitor it
- Transient errors (e.g., temporary dependency outage): ShouldDelete=false → retry. Set up metrics/alerts to avoid retry storms

Logging/observability:
- Success/failure logs include Timestamp/MessageID/Type/Version for easy traceability
- If metadata parsing fails, processing continues. Make metadata required via schema if needed

Security/IAM:
- Grant minimal SQS permissions (ReceiveMessage/DeleteMessage)
- If messages contain sensitive data, verify encryption/KMS policies

## 8) Testing/CI
- Unit tests: consumer_test.go, router_test.go
- e2e: test/e2e.sh, test/docker-compose.yaml
- GitHub Actions runs lint/test/e2e; documentation-only changes should have minimal impact

If you hit local dependency issues:
- Try go mod tidy to reconcile module state.

## 9) Extensions and best practices
- Message type/version strategy: routing key is messageType+messageVersion. For upgrades, add new Register/Schema pairs and migrate gradually
- Schema evolution: keep required minimal early; prefer backward-compatible changes
- Idempotency: use an idempotency key for side effects (e.g., DB writes) to handle duplicates safely
- Tracing: use messageId as a correlation/trace identifier

## 10) FAQ
- What if no handler is registered?
  - Missing type:version is a permanent failure and will be deleted with an error log
- What if metadata parsing fails?
  - Only a warning is logged; processing continues
- How do retries work?
  - If ShouldDelete=false, the message is not deleted; after visibility timeout it will be received again by the consumer

## 11) References
- Public types: types.go
- Router/Schema/Route: router.go
- Consumer/Start/processMessage: consumer.go
- Example: example/basic/main.go
- Tests: consumer_test.go, router_test.go
- e2e: test/e2e.sh, test/docker-compose.yaml
