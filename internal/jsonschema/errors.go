package jsonschema

import "errors"

var (
	ErrSchemaValidationSystem = errors.New("schema validation system error")
	ErrSchemaValidationFailed = errors.New("schema validation failed")
)
