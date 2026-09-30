//go:build !windows

package msgmodel

import (
	"encoding/json"
	"errors"
	"testing"
)

// The in-band payload OpenRouter sends when an upstream provider drops the
// connection mid-stream. It arrives two ways: as the raw `error` field of a chunk
// (json.RawMessage, via FromStreamError), or wrapped in a Go error. Both must
// classify as an APIError with the status attached, or the run layer cannot
// offer its bounded fresh-turn recovery and one transient blip ends the run.
const openrouter502Body = `{"code":502,"message":"Network connection lost.","metadata":{"error_type":"provider_unavailable"}}`

func assertInBand502(t *testing.T, got AssistantError) {
	t.Helper()
	if got.Name != ErrNameAPI {
		t.Fatalf("classified as %q, want %q -- the run cannot recover this", got.Name, ErrNameAPI)
	}
	var data APIError
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.StatusCode == nil || *data.StatusCode != 502 {
		t.Fatalf("StatusCode = %v, want 502 -- the run classifier keys on it", data.StatusCode)
	}
	if data.Message != "Network connection lost." {
		t.Fatalf("Message = %q", data.Message)
	}
	if data.ResponseBody == nil || *data.ResponseBody != openrouter502Body {
		t.Fatalf("ResponseBody not preserved: %v", data.ResponseBody)
	}
}

func TestOpenRouterInBand502ClassifiesAsAPIError(t *testing.T) {
	t.Run("as the raw error field of a chunk (the runtime path)", func(t *testing.T) {
		assertInBand502(t, FromError(json.RawMessage(openrouter502Body)))
	})

	t.Run("wrapped in a Go error", func(t *testing.T) {
		assertInBand502(t, FromError(errors.New(openrouter502Body)))
	})
}

// The recognizer classifies shape only; policy stays with the run. A 4xx
// in the same shape must carry its status and NOT be marked retryable here.
func TestOpenRouterInBand4xxCarriesStatusWithoutRetryFlag(t *testing.T) {
	got := FromError(json.RawMessage(`{"code":400,"message":"bad request"}`))
	if got.Name != ErrNameAPI {
		t.Fatalf("classified as %q, want %q", got.Name, ErrNameAPI)
	}
	var data APIError
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.StatusCode == nil || *data.StatusCode != 400 {
		t.Fatalf("StatusCode = %v, want 400", data.StatusCode)
	}
	if data.IsRetryable {
		t.Fatal("recognizer must not set IsRetryable; retry policy belongs to the run")
	}
}

// Shapes the recognizer must decline, so nothing that previously classified
// changes behaviour.
func TestOpenRouterInBandRecognizerDeclines(t *testing.T) {
	for name, payload := range map[string]string{
		"enveloped stream error": `{"type":"error","error":{"code":"overloaded_error","message":"x"}}`,
		"nested error object":    `{"error":{"code":502,"message":"x"}}`,
		"string code":            `{"code":"NOT_A_NUMBER","message":"x"}`,
		"no message":             `{"code":502}`,
		"non-http code":          `{"code":-32000,"message":"jsonrpc-style"}`,
		"fractional code":        `{"code":502.5,"message":"x"}`,
		"not an object":          `"Network connection lost."`,
	} {
		t.Run(name, func(t *testing.T) {
			if apiErr := openRouterInBandAPIError([]byte(payload)); apiErr != nil {
				t.Fatalf("recognized %s as %+v; must decline", payload, apiErr)
			}
		})
	}
	// And a plain Go error with a non-JSON message still degrades to
	// UnknownError exactly as before.
	got := FromError(errors.New("Network connection lost."))
	if got.Name != ErrNameUnknown {
		t.Fatalf("plain text error classified as %q, want %q", got.Name, ErrNameUnknown)
	}
}
