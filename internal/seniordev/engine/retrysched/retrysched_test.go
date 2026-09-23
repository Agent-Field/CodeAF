//go:build !windows

package retrysched

import "testing"

// apiError builds the wire-shape APIError (name + data) a provider failure
// classifies to.
func apiError(message string) Err {
	isRetryable := false
	return Err{Name: "APIError", Data: ErrData{Message: &message, IsRetryable: &isRetryable}}
}

func TestIsTimeoutError(t *testing.T) {
	t.Run("matches AbortError by name", func(t *testing.T) {
		if got := IsTimeoutError(Err{Name: "AbortError"}); got != true {
			t.Errorf("IsTimeoutError = %v, want true", got)
		}
	})

	t.Run("matches timeout-shaped messages", func(t *testing.T) {
		for _, message := range []string{
			"openrouter first-content timeout after 45000ms",
			"openrouter content-idle timeout: no data: chunks",
			"request timed out",
			"context deadline exceeded",
		} {
			if got := IsTimeoutError(apiError(message)); got != true {
				t.Errorf("IsTimeoutError(%q) = %v, want true", message, got)
			}
		}
	})

	t.Run("does not match rate-limit or generic errors", func(t *testing.T) {
		if got := IsTimeoutError(apiError("rate limit exceeded")); got != false {
			t.Errorf("IsTimeoutError(rate limit) = %v, want false", got)
		}
		if got := IsTimeoutError(apiError("Internal Server Error")); got != false {
			t.Errorf("IsTimeoutError(500) = %v, want false", got)
		}
	})

	t.Run("an absent message is not a timeout", func(t *testing.T) {
		retryable := true
		if IsTimeoutError(Err{Name: "APIError", Data: ErrData{IsRetryable: &retryable}}) {
			t.Error("IsTimeoutError with absent message = true, want false")
		}
	})
}

func TestFromStreamErrorReadsTheInBandPayload(t *testing.T) {
	classified := FromStreamError([]byte(
		`{"code":502,"message":"Network connection lost.","metadata":{"error_type":"provider_unavailable"}}`))
	if classified.Data.Message == nil || *classified.Data.Message == "" {
		t.Fatalf("in-band error lost its message: %#v", classified)
	}
	if IsContextOverflow(classified) {
		t.Fatalf("a 502 classified as a context overflow: %#v", classified)
	}
}
