//go:build !windows

package retrysched

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

func TestStatusErrorClassifiesThroughTheRouter(t *testing.T) {
	body := `{"error":{"message":"Provider returned error","error_type":"unmapped"}}`
	err := NewProviderError("429 Too Many Requests", 429, HeaderPairs(map[string][]string{"Retry-After": {"7"}}), &body)
	if !adaptive.IsLikelyRateLimit(err) {
		t.Error("a 429 must classify as a rate limit")
	}
	if !adaptive.IsLikelyTransientProviderError(err) {
		t.Error("the response body must be searched by the classifiers")
	}
	wrapped := fmt.Errorf("stream failed: %w", err)
	if !adaptive.IsLikelyRateLimit(wrapped) {
		t.Error("wrapping must not hide the status")
	}
	if err.ResponseHeaders["retry-after"] != "7" {
		t.Errorf("headers = %v", err.ResponseHeaders)
	}
}

func TestFromErrorProjectsStatusAndOverflow(t *testing.T) {
	classified := FromError(NewProviderError("Bad Gateway", 502, nil, nil))
	if !classified.IsAPIError() || classified.Data.StatusCode == nil || *classified.Data.StatusCode != 502 {
		t.Fatalf("classified = %+v", classified)
	}
	if classified.Data.IsRetryable == nil || !*classified.Data.IsRetryable {
		t.Fatalf("a 502 is retryable: %+v", classified)
	}
	overflow := FromError(NewProviderError("prompt is too long: 300000 tokens", 400, nil, nil))
	if overflow.Name != "ContextOverflowError" {
		t.Fatalf("overflow = %+v", overflow)
	}
	plain := FromError(errors.New("dial tcp: connection refused"))
	if plain.Name != "UnknownError" || plain.Data.Message == nil || *plain.Data.Message != "dial tcp: connection refused" {
		t.Fatalf("plain = %+v", plain)
	}
	if zero := FromError(nil); zero.Name != "" || zero.Data.Message != nil {
		t.Fatalf("nil classifies to the zero Err, got %+v", zero)
	}
}

func TestNamedErrorsKeepTheirName(t *testing.T) {
	abort := &StatusError{Message: "aborted", Name: "AbortError"}
	if !IsTimeoutError(FromError(abort)) {
		t.Error("an AbortError is a timeout for the step loop")
	}
	if !adaptive.IsLikelyTimeout(abort) {
		t.Error("an AbortError is a timeout for the router")
	}
}
