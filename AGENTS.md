# AGENTS — sqsrouter Codebase Guide

## Purpose
A concise, automation-friendly guide for AI agents and tooling to understand, navigate, and operate the sqsrouter repository.

## Repository
- URL: https://github.com/HatsuneMiku3939/sqsrouter
- Language: Go
- Module: github.com/hatsunemiku3939/sqsrouter
- CI: .github/workflows/test.yaml (gomod, unit, lint, e2e, examples)

## Core Components
- Router: Validates envelope, optionally validates payload, dispatches to a handler, applies Policy to produce a RoutedResult.
- Consumer: Polls SQS via long polling, invokes Router for each message, deletes message only if RoutedResult.ShouldDelete is true.
- Policy: Central decision layer for delete vs retry across failure kinds (ImmediateDeletePolicy, SQSRedrivePolicy).
- Middleware: Wraps the routing pipeline to add cross-cutting behavior.

## Message Envelope
```json
{
  "schemaVersion": "1.0",
  "messageType": "UserCreated",
  "messageVersion": "v1",
  "message": { "userId": "123", "name": "Alice" },
  "metadata": {
    "timestamp": "2024-01-01T00:00:00Z",
    "source": "svcA",
    "messageId": "uuid-..."
  }
}
```

## Key Types and APIs
- types.go
  - type MessageHandler func(ctx context.Context, messageJSON []byte, metadataJSON []byte) HandlerResult
  - type HandlerFunc func(ctx context.Context, state *RouteState) (RoutedResult, error)
  - type Middleware func(next HandlerFunc) HandlerFunc
  - type Router struct { ... }
  - type HandlerResult { ShouldDelete bool; Error error }
  - type RoutedResult { MessageType, MessageVersion string; HandlerResult; MessageID, Timestamp string }
- router.go
  - func NewRouter(envelopeSchema string, opts ...RouterOption) (*Router, error)
  - func (r *Router) Register(messageType, messageVersion string, handler MessageHandler)
  - func (r *Router) RegisterSchema(messageType, messageVersion string, schema string) error
  - func (r *Router) Use(mw ...Middleware)
  - func (r *Router) Route(ctx context.Context, rawMessage []byte) RoutedResult
  - EnvelopeSchema (JSON Schema for envelope)
- consumer/consumer.go
  - type SQSClient interface { ReceiveMessage(...); DeleteMessage(...) }
  - func NewConsumer(client SQSClient, queueURL string, router *sqsrouter.Router) *Consumer
  - func (c *Consumer) Start(ctx context.Context)
- policy/
  - type FailureKind (FailEnvelopeSchema, FailEnvelopeParse, FailPayloadSchema, FailNoHandler, FailHandlerError, FailHandlerPanic, FailMiddlewareError)
  - type Result { ShouldDelete bool; Error error }
  - type Policy interface { Decide(ctx context.Context, kind FailureKind, inner error, current Result) Result }
  - ImmediateDeletePolicy: delete on structural/permanent failures; preserve handler intent on handler/middleware errors
  - SQSRedrivePolicy: never delete on failures; rely on SQS redrive/DLQ

## Routing Pipeline (high level)
1) Validate envelope against EnvelopeSchema.
2) Unmarshal envelope; derive key = messageType:messageVersion.
3) Resolve handler and optional payload schema.
4) If schema exists, validate payload.
5) Prepare metadata JSON and call handler(message, metadata).
6) If handler error, consult Policy; else success.
7) Middlewares wrap the core; outer guard maps panics to FailHandlerPanic via Policy.

## Consumer Lifecycle
- Long polls ReceiveMessage(maxMessages=5, waitTimeSeconds=10).
- Each message processed in its own goroutine with processingTimeout=30s.
- On RoutedResult.ShouldDelete=true, DeleteMessage with deleteTimeout=5s.
- On false, message is left for retry (visibility timeout expiry).
- Graceful shutdown via context cancellation; waits for in-flight messages.

## Examples
- example/basic/main.go: Registers handler and payload schema for "updateUserProfile" v1.0, starts Consumer.
- test/e2e/: Minimal app and script to run against LocalStack.

## Common Operations

Run unit tests
```bash
make test
```

Run linters
```bash
make lint
```

Run end-to-end test (LocalStack)
```bash
make e2e-test
```

## Troubleshooting
- Reconcile module state:
```bash
go mod tidy
```
- If messages are not being deleted, check:
  - HandlerResult.ShouldDelete is true for successful/permanent outcomes
  - Selected Policy (ImmediateDeletePolicy vs SQSRedrivePolicy)
  - Consumer DeleteMessage errors in logs

## Operational Guidance
- Configure SQS visibility timeout above worst-case processing time.
- Use DLQ with appropriate maxReceiveCount for stuck messages.
- Emit logs with timestamp/messageId/type/version for correlation.
- Consider idempotency for side-effecting handlers.

## Extending
- New Policy: implement Policy.Decide and pass with WithPolicy(...) when creating Router.
- New Middleware: implement Middleware and register via router.Use(...).
- New Handlers: router.Register("Type", "Version", handler) and (optionally) RegisterSchema.

## Security
- Minimal AWS IAM permissions: ReceiveMessage, DeleteMessage for the queue.
- Ensure encryption/KMS and data handling policies for sensitive payloads.

## Notes for Automation
- Route returns a concrete RoutedResult (no error); failures are encoded in HandlerResult.Error and ShouldDelete after Policy.Decide.
- Middleware errors are mapped via Policy once.
- Panics are caught at the outer guard and mapped to FailHandlerPanic.

## Attribution
Originally written and maintained by contributors and Devin, with updates from the core team.
