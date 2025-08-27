package jsonschema

import (
	"errors"
	"testing"
)

func TestNewSchema_Valid(t *testing.T) {
	schema := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"name":{"type":"string"}}}`
	loader := NewStringLoader(schema)
	if _, err := NewSchema(loader); err != nil {
		t.Fatalf("expected valid schema, got error: %v", err)
	}
}

func TestNewSchema_Invalid(t *testing.T) {
	invalid := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{name:{"type":"string"}}}`
	loader := NewStringLoader(invalid)
	if _, err := NewSchema(loader); err == nil {
		t.Fatalf("expected schema creation error, got nil")
	}
}

func TestValidate_ValidDocument(t *testing.T) {
	schema := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`
	sLoader := NewStringLoader(schema)
	if _, err := NewSchema(sLoader); err != nil {
		t.Fatalf("schema should be valid: %v", err)
	}
	doc := []byte(`{"name":"miku"}`)
	res, err := Validate(sLoader, NewBytesLoader(doc))
	if err != nil {
		t.Fatalf("validate returned system error: %v", err)
	}
	if !res.Valid() {
		t.Fatalf("expected document to be valid, got errors: %+v", res.Errors())
	}
	if ferr := FormatErrors(res, nil); ferr != nil {
		t.Fatalf("expected FormatErrors to return nil for valid result, got: %v", ferr)
	}
}

func TestValidate_InvalidDocument(t *testing.T) {
	schema := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"age":{"type":"integer"}},"required":["age"]}`
	sLoader := NewStringLoader(schema)
	if _, err := NewSchema(sLoader); err != nil {
		t.Fatalf("schema should be valid: %v", err)
	}
	doc := []byte(`{"age":"not-integer"}`)
	res, err := Validate(sLoader, NewBytesLoader(doc))
	if err != nil {
		t.Fatalf("validate returned system error: %v", err)
	}
	if res.Valid() {
		t.Fatalf("expected document to be invalid")
	}
	ferr := FormatErrors(res, nil)
	if ferr == nil {
		t.Fatalf("expected FormatErrors to return error for invalid result")
	}
	if !errors.Is(ferr, ErrSchemaValidationFailed) {
		t.Fatalf("expected error to wrap ErrSchemaValidationFailed, got: %v", ferr)
	}
}

func TestFormatErrors_SystemError(t *testing.T) {
	sysErr := FormatErrors(nil, assertError{})
	if sysErr == nil {
		t.Fatalf("expected non-nil error for system error")
	}
	if !errors.Is(sysErr, ErrSchemaValidationSystem) {
		t.Fatalf("expected error to wrap ErrSchemaValidationSystem, got: %v", sysErr)
	}
}

type assertError struct{}

func (assertError) Error() string { return "system boom" }
