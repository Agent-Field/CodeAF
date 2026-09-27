//go:build !windows

// Package retrysched classifies the errors a model call can end in: the
// parsed Err shape, the timeout and context-overflow predicates the step loop
// consults, and the StatusError a failed provider response becomes. senior-dev
// issues one request per model call; nothing here schedules or re-issues a
// request.
package retrysched

// Err is a classified error: a name plus the payload fields the predicates
// probe.
type Err struct {
	Name string  `json:"name"`
	Data ErrData `json:"data"`
}

// ErrData models the error payload for the keys the predicates probe. Every
// field is a pointer because a provider error can omit any of them, including
// the message.
type ErrData struct {
	Message         *string           `json:"message,omitempty"`
	StatusCode      *float64          `json:"statusCode,omitempty"`
	IsRetryable     *bool             `json:"isRetryable,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    *string           `json:"responseBody,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// IsAPIError reports whether the classified error is a provider API error.
func (e Err) IsAPIError() bool { return e.Name == "APIError" }

// IsTimeoutError reports whether the classified error is timeout-shaped: an
// AbortError by name, or a timeout-shaped message. A missing message is not a
// match.
func IsTimeoutError(err Err) bool {
	if err.Name == "AbortError" {
		return true
	}
	msg := err.Data.Message
	return msg != nil && timeoutMessageRE.MatchString(*msg)
}
