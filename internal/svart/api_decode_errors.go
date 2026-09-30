package svart

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// JSON type errors refer to wire fields and JSON types, never Go DTO names.
// This keeps diagnostics stable when an implementation type is renamed.
func jsonTypeErrorMessage(err error) (string, bool) {
	var typed *json.UnmarshalTypeError
	if !errors.As(err, &typed) {
		return "", false
	}
	expected := "a compatible JSON value"
	switch typed.Type.Kind() {
	case reflect.Struct, reflect.Map:
		expected = "object"
	case reflect.Array, reflect.Slice:
		expected = "array"
	case reflect.Bool:
		expected = "boolean"
	case reflect.String:
		expected = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		expected = "integer"
	case reflect.Float32, reflect.Float64:
		expected = "number"
	}
	if typed.Field == "" {
		return "invalid JSON request body: expected " + expected, true
	}
	return fmt.Sprintf("invalid JSON request body: field %q must be %s", typed.Field, expected), true
}
