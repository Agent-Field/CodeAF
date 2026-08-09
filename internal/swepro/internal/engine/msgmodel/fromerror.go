// Error conversion ports src/session/message-v2.ts:1159-1264. Provider
// APICallError normalization is outside this package and enters through the
// one-method ProviderErrorParser seam; stream-error parsing is reproduced
// locally because it is a pure JSON decision.
package msgmodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type ErrorContext struct {
	ProviderID string
	Aborted    bool
}

type ParsedProviderError struct {
	Type            string
	Message         string
	StatusCode      *uint64
	IsRetryable     bool
	ResponseHeaders RawObject
	ResponseBody    *string
	Metadata        RawObject
}

type ProviderErrorParser interface {
	ParseAPICallError(providerID string, err error) ParsedProviderError
}

type ProviderErrorParserFunc func(providerID string, err error) ParsedProviderError

func (f ProviderErrorParserFunc) ParseAPICallError(providerID string, err error) ParsedProviderError {
	return f(providerID, err)
}

// Marker errors mirror the runtime classes checked by fromError.
type AbortFailure struct{ Message string }

func (e AbortFailure) Error() string { return e.Message }

type OutputLengthFailure struct{}

func (OutputLengthFailure) Error() string { return ErrNameMessageOutputLength }

type LoadAPIKeyFailure struct{ Message string }

func (e LoadAPIKeyFailure) Error() string { return e.Message }

type SystemFailure struct {
	Message string
	Code    string
	Syscall string
}

func (e SystemFailure) Error() string { return e.Message }

type DecompressionFailure struct {
	Message string
	Code    string
	Errno   int
	Path    string
}

func (e DecompressionFailure) Error() string { return e.Message }

type APICallFailure struct{ Cause error }

func (e APICallFailure) Error() string {
	if e.Cause == nil {
		return ""
	}
	return e.Cause.Error()
}

func (e APICallFailure) Unwrap() error { return e.Cause }

func FromError(value any, ctx ErrorContext, parser ProviderErrorParser) AssistantError {
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
	case LoadAPIKeyFailure:
		return NewProviderAuthError(ProviderAuthErrorData{ProviderID: ctx.ProviderID, Message: e.Message})
	case *LoadAPIKeyFailure:
		if e != nil {
			return NewProviderAuthError(ProviderAuthErrorData{ProviderID: ctx.ProviderID, Message: e.Message})
		}
	case SystemFailure:
		if e.Code == "ECONNRESET" {
			return connectionResetError(e)
		}
	case *SystemFailure:
		if e != nil && e.Code == "ECONNRESET" {
			return connectionResetError(*e)
		}
	case DecompressionFailure:
		if e.Code == "ZlibError" {
			return decompressionError(e, ctx.Aborted)
		}
	case *DecompressionFailure:
		if e != nil && e.Code == "ZlibError" {
			return decompressionError(*e, ctx.Aborted)
		}
	case APICallFailure:
		return parsedAPIError(parser.ParseAPICallError(ctx.ProviderID, e), e.Error())
	case *APICallFailure:
		if e != nil {
			return parsedAPIError(parser.ParseAPICallError(ctx.ProviderID, e), e.Error())
		}
	}

	if err, ok := value.(error); ok {
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
	raw, err := jscompat.Stringify(value)
	if err != nil {
		return NewUnknownError("")
	}
	return NewUnknownError(string(raw))
}

func connectionResetError(e SystemFailure) AssistantError {
	metadata, _ := jscompat.Stringify(struct {
		Code    string `json:"code"`
		Syscall string `json:"syscall"`
		Message string `json:"message"`
	}{Code: e.Code, Syscall: e.Syscall, Message: e.Message})
	return NewAPIError(APIError{
		Message: "Connection reset by server", IsRetryable: true,
		Metadata: RawObject(metadata),
	})
}

func decompressionError(e DecompressionFailure, aborted bool) AssistantError {
	if aborted {
		return NewMessageAbortedError(e.Message)
	}
	metadata, _ := jscompat.Stringify(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: e.Code, Message: e.Message})
	return NewAPIError(APIError{
		Message: "Response decompression failed", IsRetryable: true,
		Metadata: RawObject(metadata),
	})
}

func parsedAPIError(parsed ParsedProviderError, fallback string) AssistantError {
	if parsed.Message == "" {
		parsed.Message = fallback
	}
	if parsed.Type == "context_overflow" {
		return NewContextOverflowError(ContextOverflowErrorData{
			Message: parsed.Message, ResponseBody: parsed.ResponseBody,
		})
	}
	return NewAPIError(APIError{
		Message: parsed.Message, StatusCode: parsed.StatusCode,
		IsRetryable: parsed.IsRetryable, ResponseHeaders: parsed.ResponseHeaders,
		ResponseBody: parsed.ResponseBody, Metadata: parsed.Metadata,
	})
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
		result.Message = "To use Codex with your ChatGPT plan, upgrade to Plus: https://chatgpt.com/explore/plus."
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
		raw, err := jscompat.Stringify(value)
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
