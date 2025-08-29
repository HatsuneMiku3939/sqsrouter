# sqsrouter

A lightweight Go library to route and process Amazon SQS messages by type and version with optional JSON Schema validation.

[![CI](https://github.com/HatsuneMiku3939/sqsrouter/actions/workflows/test.yaml/badge.svg)](https://github.com/HatsuneMiku3939/sqsrouter/actions/workflows/test.yaml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> ⚠️ **Warning**  
> This library is under active development.  
> Interfaces, features, and behaviors may change frequently without prior notice.  
> Use with caution in production environments.

## Table of Contents
- Overview
- Features
- Quick Start
- Project Structure
- Requirements and Compatibility
- Local E2E Testing
- Development
- Contributing
- License
- Acknowledgments

## Overview
sqsrouter helps Go developers build message-driven apps on SQS. It abstracts long polling, routing, validation, and lifecycle handling so you can focus on business logic.

## Features
- Route messages by messageType and messageVersion
- Optional JSON Schema validation per type/version
- Middleware chain around routing with optional fail-fast
- Concurrent processing, timeouts, and graceful shutdown
- Clear delete vs. retry contract via handler results
- Simple, testable design

## Quick Start

Install
- This is a Go module; dependencies are resolved by `go mod`.

Define a router and register handlers
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
    // parse and process msgJSON, use metaJSON if needed
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

Run a consumer
```go
package main

import (
  "context"
  "github.com/aws/aws-sdk-go-v2/config"
  "github.com/aws/aws-sdk-go-v2/service/sqs"
  "github.com/hatsunemiku3939/sqsrouter"
)

func main() {
  cfg, err := config.LoadDefaultConfig(context.Background())
  if err != nil { panic(err) }

  client := sqs.NewFromConfig(cfg)

  router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
  if err != nil { panic(err) }

  consumer := sqsrouter.NewConsumer(client, "https://sqs.{region}.amazonaws.com/{account}/{queue}", router)
  ctx := context.Background()
  consumer.Start(ctx) // blocks; cancel ctx to stop
}
```
## Middleware

Register middlewares to wrap the routing pipeline:

```go
router.Use(TracingMW(), LoggingMW(), MetricsMW())
// or
mws := []sqsrouter.Middleware{TracingMW(), LoggingMW(), MetricsMW()}
router.Use(mws...)
```

Decision policy:

```go
// Use default ImmediateDeletePolicy (immediate deletion for permanent errors)
router, _ := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)

// Or provide a custom policy
router, _ := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema, sqsrouter.WithPolicy(MyPolicy{}))
```

### Failure Policy

By default, the router uses ImmediateDeletePolicy which immediately deletes messages for structural/permanent failures (invalid envelope/payload, no handler, panics). This conserves resources but bypasses SQS DLQ redrive for such cases.

If you prefer delegating all failures to SQS redrive so every failed message is retried per queue settings and eventually goes to the DLQ, use SQSRedrivePolicy:

```go
// Delegate all failures to SQS redrive (no immediate deletes by the consumer)
router, _ := sqsrouter.NewRouter(
    sqsrouter.EnvelopeSchema,
    sqsrouter.WithPolicy(sqsrouter.SQSRedrivePolicy{}),
)
```

- ImmediateDeletePolicy: fail-fast deletes on permanent/structural errors.
- SQSRedrivePolicy: never deletes on failures; SQS manages retries and DLQ routing.

Middlewares run even if no handler is registered, so you can log/measure such cases.

Example app
- See example/basic for a runnable example.

## Project Structure
```
sqsrouter/
├── consumer.go                 # SQS polling and lifecycle (receive/delete, timeouts, concurrency)
├── router.go                   # Routing by type/version, schema validation, handler registry
├── types.go                    # All exported public types (structs, interfaces, function types)
├── example/
│   └── basic/                  # Minimal runnable example
├── test/
│   ├── docker-compose.yaml     # LocalStack for SQS
│   ├── e2e.sh                  # End-to-end test runner
│   └── e2e/                    # E2E test application
├── .github/workflows/test.yaml # CI: lint, unit, e2e
├── .golangci.yml               # Lint configuration
├── Makefile                    # Common dev tasks
└── LICENSE
```

## Requirements and Compatibility
- Go: module declares `go 1.24`; use a current stable Go toolchain
- Dependencies:
  - AWS SDK for Go v2
  - gojsonschema for JSON Schema validation
- Production note: Set SQS visibility timeout to exceed worst-case processing time.

Breaking changes will be called out in releases.

## Local E2E Testing
Prerequisites: Docker, Docker Compose

Run:
```bash
make e2e-test
```
This starts LocalStack, runs the test app, publishes a test message, and verifies success via logs.

## Development
Common tasks:
```bash
make test              # run unit tests
make lint              # run linters (golangci-lint)
make e2e-test          # run end-to-end tests with LocalStack
```

Tips:
- If you see missing go.sum entries, run:
```bash
go mod tidy
```
- Filter tests:
```bash
make test TESTARGS="-run=MyTest"
```

## Contributing
Issues and PRs are welcome.
- Keep changes focused
- Add tests when possible
- Run lint and tests before submitting
- For larger API/behavior changes, open an issue for discussion first

## License
MIT. See LICENSE.

## Acknowledgments
Originally written and maintained by contributors and Devin, with updates from the core team. Initial development supported by Google Gemini.
