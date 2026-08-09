// Package retry is the session-facing surface for src/session/retry.ts. The
// bug-for-bug implementation and its differential corpus live in
// internal/engine/retrysched.
package retry

import (
	"encoding/json"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/retrysched"
)

const (
	GO_UPSELL_MESSAGE           = retrysched.GoUpsellMessage
	GO_UPSELL_URL               = retrysched.GoUpsellURL
	RETRY_INITIAL_DELAY         = retrysched.RetryInitialDelay
	RETRY_BACKOFF_FACTOR        = retrysched.RetryBackoffFactor
	RETRY_MAX_DELAY_NO_HEADERS  = retrysched.RetryMaxDelayNoHeaders
	RETRY_MAX_DELAY             = retrysched.RetryMaxDelay
	TIMEOUT_RETRY_DELAY_DEFAULT = retrysched.TimeoutRetryDelayDefault
)

type RetryReason = retrysched.RetryReason
type RetryAction = retrysched.RetryAction
type RetryableInfo = retrysched.RetryInfo
type Err = retrysched.Err
type ErrData = retrysched.ErrData
type Headers = retrysched.Headers
type Decision = retrysched.Decision

func NewHeaders(pairs ...string) *Headers { return retrysched.NewHeaders(pairs...) }

func IsTimeoutError(err Err) bool { return retrysched.IsTimeoutError(err) }

func IsContextOverflow(err Err) bool { return retrysched.IsContextOverflow(err) }

func Delay(attempt float64, err *Err, isTimeout bool) float64 {
	return retrysched.Delay(attempt, err, isTimeout)
}

func Retryable(err Err, provider string) *RetryableInfo {
	return retrysched.Retryable(err, provider)
}

func FromError(err error) Err { return retrysched.FromError(err) }

func FromStreamError(raw json.RawMessage) Err {
	parsed := msgmodel.FromError(raw, msgmodel.ErrorContext{}, nil)
	var data ErrData
	_ = json.Unmarshal(parsed.Data, &data)
	return Err{Name: parsed.Name, Data: data}
}

func HeaderPairs(headers map[string][]string) *Headers { return retrysched.HeaderPairs(headers) }

// ClampProviderSuggestedDelay is a Go-only hot-loop guard. Formula-derived
// backoff is unchanged; only an explicit Retry-After header at or below zero
// is floored.
func ClampProviderSuggestedDelay(wait float64, err Err) float64 {
	if wait > 0 || !err.IsAPIError() || err.Data.ResponseHeaders == nil {
		return wait
	}
	headers := err.Data.ResponseHeaders
	if headers.Get("retry-after-ms") != "" || headers.Get("retry-after") != "" {
		return 250
	}
	return wait
}

func NewProviderError(message string, status float64, headers *Headers, body *string) error {
	return retrysched.NewProviderError(message, status, headers, body)
}

type PolicyOptions struct {
	Provider string
	Parse    func(input any) Err
	Set      func(Decision) error
}

// PolicyStep is one Schedule.fromStepWithMetadata evaluation. A false retry
// is Cause.done; otherwise wait is the returned Duration in milliseconds.
func PolicyStep(
	attempt float64, input any, options PolicyOptions,
) (wait float64, retry bool, err error) {
	classified := options.Parse(input)
	decision := retrysched.Step(attempt, classified, options.Provider)
	if decision == nil {
		return 0, false, nil
	}
	if options.Set != nil {
		if err := options.Set(*decision); err != nil {
			return 0, false, err
		}
	}
	return decision.Wait, true, nil
}
