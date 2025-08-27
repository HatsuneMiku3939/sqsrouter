package jsonschema

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

type (
	ValidationResult = gojsonschema.Result
	JSONLoader       = gojsonschema.JSONLoader
)

func NewStringLoader(s string) gojsonschema.JSONLoader {
	return gojsonschema.NewStringLoader(s)
}

func NewBytesLoader(b []byte) gojsonschema.JSONLoader {
	return gojsonschema.NewBytesLoader(b)
}

func NewSchema(loader gojsonschema.JSONLoader) (*gojsonschema.Schema, error) {
	return gojsonschema.NewSchema(loader)
}

func Validate(schemaLoader gojsonschema.JSONLoader, docLoader gojsonschema.JSONLoader) (*gojsonschema.Result, error) {
	return gojsonschema.Validate(schemaLoader, docLoader)
}

func FormatErrors(result *gojsonschema.Result, err error) error {
	if err != nil {
		return fmt.Errorf("schema validation system error: %v", err)
	}
	if result.Valid() {
		return nil
	}
	var errMsg string
	for _, desc := range result.Errors() {
		errMsg += fmt.Sprintf("- %s; ", desc)
	}
	return fmt.Errorf("schema validation failed: %s", errMsg)
}
