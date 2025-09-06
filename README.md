# sqsrouter

A Go library to route and process Amazon SQS messages by type and version, with optional JSON Schema validation and pluggable failure policies.

[![CI](https://github.com/HatsuneMiku3939/sqsrouter/actions/workflows/test.yaml/badge.svg)](https://github.com/HatsuneMiku3939/sqsrouter/actions/workflows/test.yaml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> ⚠️ **Warning**  
> This library is under active development.  
> Interfaces, features, and behaviors may change frequently without prior notice.  
> Use with caution in production environments.

- Message routing by messageType and messageVersion
- Optional JSON Schema validation per type/version
- Composable middleware chain
- Clear delete vs retry contract via FailurePolicy and handler results
- Concurrent processing, timeouts, and graceful shutdown

## Table of Contents
- [Overview](#overview)
- [Quick Start](#quick-start)
- [Usage](#usage)
- [Middleware](#middleware)
- [Failure Policies](#failure-policies)
- [Project Structure](#project-structure)
- [Requirements](#requirements)
- [Local E2E Testing](#local-e2e-testing)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)
- [Attribution](#attribution)

## Overview
sqsrouter abstracts SQS long polling, message routing, schema validation, and lifecycle handling so teams can focus on business logic instead of plumbing.

## Quick Start

### Install
This is a Go module. Use Go 1.24.x and standard tooling.

### Minimal router and handler
```go
package main

import (
  "context"
  "github.com/hatsunemiku3939/sqsrouter"
)

func main() {
  router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
  if err != nil {
    panic(err) // handle properly in production
  }

  router.Register("UserCreated", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) sqsrouter.HandlerResult {
    // parse and process msgJSON; metaJSON contains envelope metadata
    return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
  })

  schema := `{
    "$schema":"http://json-schema.org/draft-07/schema#",
    "type":"object",
    "properties":{ "userId":{"type":"string"}, "name":{"type":"string"} },
    "required":["userId","name"]
  }`
  if err := router.RegisterSchema("UserCreated", "v1", schema); err != nil {
    panic(err)
  }
}
```

### Run a consumer
```go
package main

import (
  "context"
  "github.com/aws/aws-sdk-go-v2/config"
  "github.com/aws/aws-sdk-go-v2/service/sqs"
  "github.com/hatsunemiku3939/sqsrouter"
  "github.com/hatsunemiku3939/sqsrouter/consumer"
)

func main() {
  cfg, err := config.LoadDefaultConfig(context.Background())
  if err != nil { panic(err) }
  client := sqs.NewFromConfig(cfg)

  router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
  if err != nil { panic(err) }

  c := consumer.NewConsumer(client, "https://sqs.{region}.amazonaws.com/{account}/{queue}", router)
  ctx := context.Background()
  c.Start(ctx) // blocks until ctx is canceled
}
```

## Usage

### Message envelope used for routing
```json
{
  "schemaVersion": "1.0",
  "messageType": "UserCreated",
  "messageVersion": "v1",
  "message": { "userId": "123", "name": "Alice" },
  "metadata": { "timestamp": "2024-01-01T00:00:00Z", "source": "svcA", "messageId": "uuid-..." }
}
```

### Handler contract
- ShouldDelete=true for success or permanent failures (do not retry).
- ShouldDelete=false for transient failures (allow retry when visibility timeout expires).
- Error is attached on failure; nil means success.

## Middleware

Register middlewares to wrap the routing pipeline:
```go
router.Use(TracingMW(), LoggingMW(), MetricsMW())

mws := []sqsrouter.Middleware{TracingMW(), LoggingMW(), MetricsMW()}
router.Use(mws...)
```

- Middlewares run in registration order and wrap core routing.
- Middlewares can read RouteState and adjust RoutedResult.
- Middlewares run even when a handler is not registered.

## Failure Policies

### Default: ImmediateDeletePolicy
- Deletes on structural/permanent failures:
  - Invalid envelope schema, envelope parse failure
  - Invalid payload schema
  - No handler registered
  - Handler panic
- Preserves handler intent for HandlerError or MiddlewareError.

```go
router, _ := sqsrouter.NewRouter(
  sqsrouter.EnvelopeSchema,
  sqsrouter.WithFailurePolicy(sqsrouter.ImmediateDeletePolicy{}),
)
```

### Alternative: SQSRedrivePolicy
- Never deletes on failures; retries and DLQ routing are delegated to SQS redrive.

```go
router, _ := sqsrouter.NewRouter(
  sqsrouter.EnvelopeSchema,
  sqsrouter.WithFailurePolicy(sqsrouter.SQSRedrivePolicy{}),
)
```

## Routing Policies

Customize how handlers are selected for a message. Default is exact match on `messageType:messageVersion`.

```go
// Explicitly use ExactMatchPolicy (same as default behavior):
router, _ := sqsrouter.NewRouter(
  sqsrouter.EnvelopeSchema,
  sqsrouter.WithRoutingPolicy(sqsrouter.ExactMatchPolicy{}),
)
```

```go
// Define a custom routing policy by implementing sqsrouter.RoutingPolicy
type MyRoutingPolicy struct{}
func (MyRoutingPolicy) Decide(ctx context.Context, env *sqsrouter.MessageEnvelope, available []sqsrouter.HandlerKey) sqsrouter.HandlerKey {
  // Example: fallback to v1 if exact match not found
  want := sqsrouter.HandlerKey(env.MessageType + ":" + env.MessageVersion)
  for _, k := range available { if k == want { return k } }
  fallback := sqsrouter.HandlerKey(env.MessageType + ":v1")
  for _, k := range available { if k == fallback { return k } }
  return ""
}

router, _ := sqsrouter.NewRouter(
  sqsrouter.EnvelopeSchema,
  sqsrouter.WithRoutingPolicy(MyRoutingPolicy{}),
)
```

## Project Structure
```
sqsrouter/
├── consumer/                   # SQS polling and lifecycle (receive/delete, timeouts, concurrency)
├── internal/jsonschema/        # JSON schema validation utilities
├── router.go                   # Routing by type/version, schema validation, handler registry
├── types.go                    # Public types and interfaces
├── failure.go                  # Failure types and interfaces
├── failure_policy_*.go         # Built-in failure policies
├── routing_exact_match.go      # Default exact-match routing policy
├── example/
│   └── basic/                  # Minimal runnable example
├── test/
│   ├── docker-compose.yaml     # LocalStack for SQS
│   ├── e2e.sh                  # End-to-end test runner
│   └── e2e/                    # E2E test application
└── .github/workflows/test.yaml # CI: lint, unit, e2e
```

## Requirements
- Go: 1.24.x (see go.mod)
- AWS SDK for Go v2
- gojsonschema
- Set SQS visibility timeout above worst-case processing time; use DLQ + redrive policy in production.

## Local E2E Testing
Requires Docker and Docker Compose.
```bash
make e2e-test
```
This starts LocalStack, runs a test app, publishes a test message, and verifies via logs.

## Development
```bash
make test              # unit tests
make lint              # golangci-lint
make e2e-test          # end-to-end test with LocalStack
```
If dependencies drift:
```bash
go mod tidy
```
Filter tests:
```bash
make test TESTARGS="-run=MyTest"
```

## Contributing
- Issues and PRs welcome
- Keep changes focused and add tests when possible
- Run lint and tests before submitting
- Discuss larger API changes in an issue first

## License
MIT. See LICENSE.

## Attribution
This project was created and maintained with the help of AI tools — Devin, Gemini, and Codex — under my guidance. 
Specifications were defined through conversations with AI, and implementations were carried out by the AI.
