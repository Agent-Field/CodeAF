//go:build !windows

// Error formatting
package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// StackError may supply a pre-recorded stack trace.
type StackError interface {
	error
	Stack() string
}

func ErrorFormat(value any) string {
	if err, ok := value.(error); ok {
		if stack, ok := err.(StackError); ok && stack.Stack() != "" {
			return stack.Stack()
		}
		name := reflect.TypeOf(err).String()
		name = strings.TrimPrefix(name, "*")
		if named, ok := err.(interface{ ErrorName() string }); ok {
			name = named.ErrorName()
		}
		return name + ": " + err.Error()
	}
	if IsRecord(value) {
		data, err := jsonutil.MarshalIndent(value)
		if err != nil {
			return "Unexpected error (unserializable)"
		}
		if string(data) == "{}" {
			t := reflect.TypeOf(value)
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			prefix := t.Name()
			if prefix == "" {
				prefix = "Error"
			}
			names := ownPropertyNames(value)
			if len(names) == 0 {
				return prefix + " (no message)"
			}
			return prefix + " { " + strings.Join(names, ", ") + " }"
		}
		return string(data)
	}
	return stringifyValue(value)
}

func ErrorMessage(value any) string {
	if err, ok := value.(error); ok {
		if err.Error() != "" {
			return err.Error()
		}
		name := reflect.TypeOf(err).String()
		if name != "" {
			return strings.TrimPrefix(name, "*")
		}
	}
	if record, ok := stringMap(value); ok {
		if message, ok := record["message"].(string); ok && message != "" {
			return message
		}
		if data, ok := stringMap(record["data"]); ok {
			if message, ok := data["message"].(string); ok && message != "" {
				return message
			}
		}
	}
	text := stringifyValue(value)
	if text != "" && text != "[object Object]" {
		return text
	}
	if formatted := ErrorFormat(value); formatted != "" {
		return formatted
	}
	return "unknown error"
}

func ErrorData(value any) map[string]any {
	if err, ok := value.(error); ok {
		name := reflect.TypeOf(err).String()
		name = strings.TrimPrefix(name, "*")
		out := map[string]any{
			"type":      name,
			"message":   ErrorMessage(err),
			"formatted": ErrorFormat(err),
		}
		if stack, ok := err.(StackError); ok {
			out["stack"] = stack.Stack()
		}
		if cause := errors.Unwrap(err); cause != nil {
			out["cause"] = ErrorFormat(cause)
		}
		return out
	}
	if !IsRecord(value) {
		return map[string]any{
			"type":      valueTypeName(value),
			"message":   ErrorMessage(value),
			"formatted": ErrorFormat(value),
		}
	}
	out := map[string]any{}
	if record, ok := stringMap(value); ok {
		for key, item := range record {
			switch typed := item.(type) {
			case nil:
				out[key] = "null"
			case string, float64, float32, int, int64, bool:
				out[key] = typed
			case error:
				out[key] = typed.Error()
			default:
				out[key] = stringifyValue(typed)
			}
		}
	}
	if _, ok := out["message"].(string); !ok {
		out["message"] = ErrorMessage(value)
	}
	if _, ok := out["type"].(string); !ok {
		t := reflect.TypeOf(value)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		name := t.Name()
		if name == "" && t.Kind() == reflect.Map {
			name = "Object"
		}
		out["type"] = name
	}
	out["formatted"] = ErrorFormat(value)
	return out
}

// stringifyValue renders a value the way error payloads expect: nil as
// "null", records as "[object Object]", scalars as their plain text.
func stringifyValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 64)
	case error:
		return typed.Error()
	}
	if IsRecord(value) {
		return "[object Object]"
	}
	return fmt.Sprint(value)
}

// valueTypeName is the coarse type label reported for non-record error
// values; nil and unknown kinds report "object".
func valueTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "object"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int64, uint, uint64:
		return "number"
	case func():
		return "function"
	default:
		return "object"
	}
}

func stringMap(value any) (map[string]any, bool) {
	if direct, ok := value.(map[string]any); ok {
		return direct, true
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil {
		return nil, false
	}
	return out, out != nil
}

func ownPropertyNames(value any) []string {
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() == reflect.Map {
		out := []string{}
		iter := v.MapRange()
		for iter.Next() {
			out = append(out, fmt.Sprint(iter.Key().Interface()))
		}
		return out
	}
	if v.Kind() == reflect.Struct {
		out := []string{}
		for i := 0; i < v.NumField(); i++ {
			out = append(out, v.Type().Field(i).Name)
		}
		return out
	}
	return nil
}

// CaptureStack provides a convenient StackError stack for callers that need
// ErrorFormat's stack branch.
func CaptureStack() string { return string(debug.Stack()) }
