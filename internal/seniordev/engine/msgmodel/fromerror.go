//go:build !windows

// Error conversion. Stream-error parsing lives here because it is a pure JSON
// decision.
package msgmodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// Marker errors FromError classifies by type.
type AbortFailure struct{ Message string }

func (e AbortFailure) Error() string { return e.Message }

type OutputLengthFailure struct{}

func (OutputLengthFailure) Error() string { return ErrNameMessageOutputLength }

// FromError classifies a failure value into the persisted AssistantError
// shape. `value` is the raw `error` payload of a model stream, a Go error, or
// one of the marker types above.
func FromError(value any) AssistantError {
	switch e := value.(type) {
	case AbortFailure:
		return NewMessageAbortedError(e.Message)
	case *AbortFailure:
		if e != nil {
			return NewMessageAbortedError(e.Message)
		}
	case OutputLengthFailure, *OutputLengthFailure:
		return NewMessageOutputLengthError()
	case AssistantError:
		if e.Name == ErrNameMessageOutputLength {
			return e
		}
	case *AssistantError:
		if e != nil && e.Name == ErrNameMessageOutputLength {
			return *e
		}
	}

	if err, ok := value.(error); ok {
		// Recognize the OpenRouter in-band shape first so a provider failure
		// wrapped in a Go error classifies as an APIError.
		if apiErr := openRouterInBandAPIError(streamJSON(err.Error())); apiErr != nil {
			return NewAPIError(*apiErr)
		}
		return NewUnknownError(errorMessage(err))
	}
	if parsed := ParseStreamError(value); parsed != nil {
		if parsed.Type == "context_overflow" {
			return NewContextOverflowError(ContextOverflowErrorData{
				Message: parsed.Message, ResponseBody: parsed.ResponseBody,
			})
		}
		return NewAPIError(APIError{
			Message: parsed.Message, IsRetryable: parsed.IsRetryable,
			ResponseBody: parsed.ResponseBody,
		})
	}
	// The raw `error` field of an OpenRouter chunk arrives here as a
	// json.RawMessage. ParseStreamError has already declined it (no envelope);
	// recognize the bare in-band shape before it degrades to UnknownError.
	if apiErr := openRouterInBandAPIError(streamJSON(value)); apiErr != nil {
		return NewAPIError(*apiErr)
	}
	raw, err := jsonutil.Marshal(value)
	if err != nil {
		return NewUnknownError("")
	}
	return NewUnknownError(string(raw))
}

func errorMessage(err error) string {
	if err == nil {
		return "Error"
	}
	if message := err.Error(); message != "" {
		return message
	}
	return fmt.Sprintf("%T", err)
}

type ParsedStreamError struct {
	Type         string
	Message      string
	IsRetryable  bool
	ResponseBody *string
}

func ParseStreamError(input any) *ParsedStreamError {
	raw := streamJSON(input)
	if len(raw) == 0 {
		return nil
	}
	var outer json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil
	}
	body := compactJSONValue(outer)
	var probe struct {
		Message any `json:"message"`
	}
	if err := json.Unmarshal(body, &probe); err == nil {
		if message, ok := probe.Message.(string); ok {
			nested := streamJSON(message)
			if len(nested) > 0 {
				var nestedValue json.RawMessage
				if json.Unmarshal(nested, &nestedValue) == nil && isJSONObject(nestedValue) {
					body = compactJSONValue(nestedValue)
				}
			}
		}
	}

	var envelope struct {
		Type  string `json:"type"`
		Error struct {
			Code    string `json:"code"`
			Message any    `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Type != "error" {
		return nil
	}
	response := string(body)
	message, _ := envelope.Error.Message.(string)
	result := &ParsedStreamError{ResponseBody: &response}
	switch envelope.Error.Code {
	case "context_length_exceeded":
		result.Type = "context_overflow"
		result.Message = "Input exceeds context window of this model"
	case "insufficient_quota":
		result.Type = "api_error"
		result.Message = "Quota exceeded. Check your plan and billing details."
	case "usage_not_included":
		result.Type = "api_error"
		result.Message = "Usage is not included in the current plan."
	case "invalid_prompt":
		result.Type = "api_error"
		result.Message = message
		if result.Message == "" {
			result.Message = "Invalid prompt."
		}
	case "server_is_overloaded", "server_error":
		result.Type = "api_error"
		result.Message = message
		if result.Message == "" {
			result.Message = "Server error."
		}
		result.IsRetryable = true
	default:
		return nil
	}
	return result
}

func streamJSON(input any) []byte {
	switch value := input.(type) {
	case json.RawMessage:
		if json.Valid(value) {
			return value
		}
	case RawObject:
		if json.Valid(value) {
			return value
		}
	case []byte:
		if json.Valid(value) {
			return value
		}
	case string:
		trimmed := strings.TrimSpace(value)
		if json.Valid([]byte(trimmed)) {
			return []byte(trimmed)
		}
	default:
		raw, err := jsonutil.Marshal(value)
		if err == nil && json.Valid(raw) {
			return raw
		}
	}
	return nil
}

func compactJSONValue(raw []byte) []byte {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return raw
	}
	return buffer.Bytes()
}

func isJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}
