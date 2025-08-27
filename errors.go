package sqsrouter

import "errors"

var (
	ErrInvalidEnvelopeSchema  = errors.New("invalid envelope schema")
	ErrInvalidSchema          = errors.New("invalid schema")
	ErrSchemaValidationSystem = errors.New("schema validation system error")
	ErrSchemaValidationFailed = errors.New("schema validation failed")
	ErrInvalidEnvelope        = errors.New("invalid envelope")
	ErrFailedToParseEnvelope  = errors.New("failed to parse envelope")
	ErrInvalidMessagePayload  = errors.New("invalid message payload")
	ErrNoHandlerRegistered    = errors.New("no handler registered")
	ErrMiddleware             = errors.New("middleware")
)
