package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hatsunemiku3939/sqsrouter"
)

const (
	MsgTypeE2ETest = "e2eTest"
	MsgVersion1_0  = "1.0"
)

// E2ETestMessage defines the structure for the "e2eTest" message payload.
type E2ETestMessage struct {
	TestID  string `json:"testId"`
	Payload string `json:"payload"`
}

// E2ETestHandler handles the logic for the e2e test message.
func E2ETestHandler(ctx context.Context, messageJSON []byte, metadataJSON []byte) sqsrouter.HandlerResult {
	var msg E2ETestMessage
	if err := json.Unmarshal(messageJSON, &msg); err != nil {
		return sqsrouter.HandlerResult{ShouldDelete: true, Error: fmt.Errorf("failed to unmarshal e2e test message: %w", err)}
	}

	// For the e2e test, we just log the message content.
	// The test script will check the log output for this message.
	log.Printf("E2E_TEST_SUCCESS: Received message for test ID %s with payload: %s", msg.TestID, msg.Payload)
	// If enabled, force an application-level error to exercise policy behavior.
	if os.Getenv("E2E_HANDLER_FORCE_ERR") == "1" {
		return sqsrouter.HandlerResult{ShouldDelete: true, Error: fmt.Errorf("e2e handler forced error")}
	}
	return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
}

type e2eMiddleware struct{}

func (e e2eMiddleware) handler(next sqsrouter.HandlerFunc) sqsrouter.HandlerFunc {
	return func(ctx context.Context, s *sqsrouter.RouteState) (sqsrouter.RoutedResult, error) {
		if s != nil && s.Envelope != nil {
			log.Printf("E2E_MW_BEFORE type=%s version=%s", s.Envelope.MessageType, s.Envelope.MessageVersion)
		} else {
			log.Printf("E2E_MW_BEFORE type=? version=?")
		}

		if os.Getenv("E2E_MW_FAIL") == "1" {
			err := fmt.Errorf("e2e middleware forced failure")
			mt := "unknown"
			mv := "unknown"
			if s != nil && s.Envelope != nil {
				mt = s.Envelope.MessageType
				mv = s.Envelope.MessageVersion
			}
			rr := sqsrouter.RoutedResult{
				MessageType:    mt,
				MessageVersion: mv,
				HandlerResult: sqsrouter.HandlerResult{
					ShouldDelete: false,
					Error:        err,
				},
			}
			log.Printf("E2E_MW_AFTER_ERR err=%v", err)
			return rr, err
		}

		rr, err := next(ctx, s)

		if os.Getenv("E2E_MW_TOUCH") == "1" {
			rr.MessageID = "MW_OVERRIDDEN"
			log.Printf("MW_OVERRIDDEN")
		}

		if err != nil {
			log.Printf("E2E_MW_AFTER_ERR err=%v", err)
		} else {
			log.Printf("E2E_MW_AFTER_OK type=%s version=%s", rr.MessageType, rr.MessageVersion)
		}

		if os.Getenv("E2E_MW_SET_DELETE") == "1" {
			rr.HandlerResult.ShouldDelete = true
		}

		return rr, err
	}
}

func E2EMiddleware() sqsrouter.Middleware {
	mw := e2eMiddleware{}
	return func(next sqsrouter.HandlerFunc) sqsrouter.HandlerFunc { return mw.handler(next) }
}

// forceRetryOnHandlerErr is a custom Policy used in E2E to demonstrate that
// handler errors are passed through Policy and can be centrally overridden.
type forceRetryOnHandlerErr struct{}

// Decide implements the Policy interface for the custom behavior.
func (forceRetryOnHandlerErr) Decide(_ context.Context, _ *sqsrouter.RouteState, kind sqsrouter.FailureKind, inner error, rr sqsrouter.RoutedResult) sqsrouter.RoutedResult {
	if kind == sqsrouter.FailHandlerError {
		rr.HandlerResult.ShouldDelete = false
		if inner != nil && rr.HandlerResult.Error == nil {
			rr.HandlerResult.Error = inner
		}
		return rr
	}
	return rr
}

func main() {
	appCtx, cancelApp := context.WithCancel(context.Background())
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-shutdownChan
		log.Printf("Received shutdown signal: %v. Starting graceful shutdown...", sig)
		cancelApp()
	}()

	awsEndpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if awsEndpointURL == "" {
		log.Fatal("AWS_ENDPOINT_URL environment variable is not set.")
	}

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:           awsEndpointURL,
			SigningRegion: region,
			PartitionID:   "aws",
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(appCtx, config.WithEndpointResolverWithOptions(customResolver))
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("SQS_QUEUE_URL environment variable is not set.")
	}

	sqsClient := sqs.NewFromConfig(cfg)

	// Optionally install a custom policy that forces retry for handler errors.
	var opts []sqsrouter.RouterOption
	if os.Getenv("E2E_POLICY_FORCE_RETRY_ON_HANDLER_ERR") == "1" {
		// Custom policy: turn any handler error into a retry (ShouldDelete=false)
		opts = append(opts, sqsrouter.WithPolicy(forceRetryOnHandlerErr{}))
	}

	router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema, opts...)
	if err != nil {
		log.Fatalf("Could not initialize router: %v", err)
	}

	testSchema := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"required": ["testId", "payload"],
		"properties": {
			"testId": { "type": "string" },
			"payload": { "type": "string" }
		},
		"additionalProperties": false
	}`
	router.RegisterSchema(MsgTypeE2ETest, MsgVersion1_0, testSchema)

	router.Use(E2EMiddleware())

	router.Register(MsgTypeE2ETest, MsgVersion1_0, E2ETestHandler)

	consumer := sqsrouter.NewConsumer(sqsClient, queueURL, router)
	consumer.Start(appCtx)

	log.Println("Application has shut down.")
}
