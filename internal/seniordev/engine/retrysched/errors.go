//go:build !windows

package retrysched

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// timeoutMessageRE matches the timeout-shaped messages a transport or a
// provider produces.
var timeoutMessageRE = regexp.MustCompile(`(?i)timeout|timed out|deadline exceeded`)

// StatusError is the error the OpenRouter path returns for a failed HTTP
// response: a message plus the status, headers and body the response carried.
// It exposes the status, name, body and cause through the small interfaces
// the adaptive router's classifiers probe.
type StatusError struct {
	Message         string
	Name            string
	Status          *float64
	Cause           error
	ResponseHeaders map[string]string
	ResponseBody    *string
}

func (e *StatusError) Error() string { return e.Message }

// ErrorName is the symbolic error name; it defaults to "Error".
func (e *StatusError) ErrorName() string {
	if e.Name == "" {
		return "Error"
	}
	return e.Name
}

// ErrorStatusCode reports the HTTP status, when the error carries one.
func (e *StatusError) ErrorStatusCode() (float64, bool) {
	if e.Status == nil {
		return 0, false
	}
	return *e.Status, true
}

// ErrorDetail is the response body, so classifiers can match provider
// messages that only appear there.
func (e *StatusError) ErrorDetail() string {
	if e.ResponseBody == nil {
		return ""
	}
	return *e.ResponseBody
}

func (e *StatusError) Unwrap() error { return e.Cause }

// NewProviderError builds the StatusError for a failed HTTP response.
func NewProviderError(message string, status float64, headers map[string]string, body *string) *StatusError {
	return &StatusError{
		Message: message, Status: &status,
		ResponseHeaders: headers, ResponseBody: body,
	}
}

// RetryError projects the failure into the classified Err shape: an APIError,
// or a ContextOverflowError when the message or body says the prompt did not
// fit.
func (e *StatusError) RetryError() Err {
	if e.Status == nil {
		// Without an HTTP status this is not a provider API error; keep the
		// symbolic name (AbortError, for one) the step loop classifies by.
		return Err{Name: e.ErrorName(), Data: ErrData{Message: &e.Message, ResponseBody: e.ResponseBody}}
	}
	status := *e.Status
	retryable := status == 408 || status == 409 || status == 429 || status >= 500
	result := Err{Name: "APIError", Data: ErrData{
		Message: &e.Message, StatusCode: e.Status, IsRetryable: &retryable,
		ResponseHeaders: e.ResponseHeaders, ResponseBody: e.ResponseBody,
	}}
	if IsContextOverflow(result) {
		return Err{Name: "ContextOverflowError", Data: ErrData{
			Message: &e.Message, ResponseBody: e.ResponseBody,
		}}
	}
	return result
}

// FromError classifies a Go error into the Err shape.
func FromError(err error) Err {
	if err == nil {
		return Err{}
	}
	var classified interface{ RetryError() Err }
	if errors.As(err, &classified) {
		return classified.RetryError()
	}
	name := "UnknownError"
	var named interface{ ErrorName() string }
	if errors.As(err, &named) && named.ErrorName() != "" {
		name = named.ErrorName()
	}
	message := err.Error()
	return Err{Name: name, Data: ErrData{Message: &message}}
}

// FromStreamError classifies an in-band error payload from a model stream.
func FromStreamError(raw json.RawMessage) Err {
	parsed := msgmodel.FromError(raw)
	var data ErrData
	_ = json.Unmarshal(parsed.Data, &data)
	return Err{Name: parsed.Name, Data: data}
}

// HeaderPairs flattens response headers into a lowercase-keyed map.
func HeaderPairs(headers map[string][]string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		out[strings.ToLower(key)] = strings.Join(values, ", ")
	}
	return out
}
