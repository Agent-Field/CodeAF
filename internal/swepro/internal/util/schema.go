// Portable schema utilities for the ports of src/util/schema.ts:4-107 and
// src/util/effect-zod.ts:1-370 (swe-pro 3b25a1a). Effect Schema and Zod are
// TypeScript-only dependencies, so the Go boundary is the narrow Validator
// interface consumed by this repository.
package util

import (
	"fmt"
	"math"
)

// Validator is the common Effect-Schema/Zod operation used at Go call sites.
type Validator interface {
	Parse(any) (any, error)
	JSONSchema() any
}

// ValidatorFunc adapts functions to Validator.
type ValidatorFunc struct {
	ParseFunc      func(any) (any, error)
	JSONSchemaFunc func() any
}

func (v ValidatorFunc) Parse(input any) (any, error) {
	if v.ParseFunc == nil {
		return input, nil
	}
	return v.ParseFunc(input)
}

func (v ValidatorFunc) JSONSchema() any {
	if v.JSONSchemaFunc == nil {
		return map[string]any{}
	}
	return v.JSONSchemaFunc()
}

// Zod is the identity adapter at the Go schema boundary.
func Zod(schema Validator) Validator { return schema }

// ZodObject requires the adapted schema's JSON Schema to be an object.
func ZodObject(schema Validator) (Validator, error) {
	jsonSchema, ok := schema.JSONSchema().(map[string]any)
	if !ok || jsonSchema["type"] != "object" {
		return nil, fmt.Errorf("Expected object schema, got non-object")
	}
	return schema, nil
}

func ToJSONSchema(schema Validator) any { return schema.JSONSchema() }

func PositiveInt(value any) (int64, bool) {
	number, ok := integer(value)
	return number, ok && number > 0
}

func NonNegativeInt(value any) (int64, bool) {
	number, ok := integer(value)
	return number, ok && number >= 0
}

func integer(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) ||
			number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

// OptionalOmitUndefined is represented by pointer/nil at Go encoding sites;
// validation is otherwise unchanged.
func OptionalOmitUndefined(schema Validator) Validator { return schema }

// WithStatics applies a caller's method attachment to a schema value.
func WithStatics[S any, R any](schema S, methods func(S) R) (S, R) {
	return schema, methods(schema)
}

// Newtype is a nominal scalar wrapper for boundaries that need a distinct Go
// type while retaining the raw value.
type Newtype[T any] struct {
	Value T
}

func MakeNewtype[T any](value T) Newtype[T] { return Newtype[T]{Value: value} }
