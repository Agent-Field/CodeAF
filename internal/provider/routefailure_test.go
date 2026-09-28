package provider

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// EVERY PROVIDER'S WORDS FOR ONE FAILURE READ AS ONE KIND. The bodies are the
// shapes each provider answers with — OpenRouter's envelope, OpenAI's, a
// Fireworks-style detail, Anthropic's — and the router downstream sees only
// the kind.
func TestRouteFailureReadsEachProvidersWordsAsOneKind(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   RouteFailure
	}{
		{"openrouter no credit", 402, `{"error":{"message":"Insufficient credits. Add more using https://openrouter.ai/settings/credits","code":402}}`, RoutePayment},
		{"openai quota is a balance", 429, `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","code":"insufficient_quota"}}`, RoutePayment},
		{"anthropic low balance", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API."}}`, RoutePayment},
		{"bad key", 401, `{"error":{"message":"No auth credentials found","code":401}}`, RouteAuth},
		{"free pool for harnesses only", 403, `{"error":{"message":"thinkingmachines/inkling-small:free is only available on agentic harnesses","code":403}}`, RouteForbidden},
		{"privacy excluded every endpoint", 404, `{"error":{"message":"No endpoints found matching your data policy (Free model publication). Configure: https://openrouter.ai/settings/privacy","code":404}}`, RouteForbidden},
		{"rate limited", 429, `{"error":{"message":"Rate limit exceeded: free-models-per-day. ","code":429}}`, RouteQuota},
		{"model gone", 404, `{"error":{"code":"NOT_FOUND","message":"Model not found, inaccessible, and/or not deployed"}}`, RouteUnavailable},
		{"upstream 5xx", 503, `{"error":{"message":"Service Unavailable","code":503}}`, RouteTransient},
		{"our own bytes", 400, `{"error":{"message":"messages: at least one message is required","code":400}}`, ""},
	}
	for _, tc := range cases {
		got, _ := RouteFailureOf(apiError(tc.status, []byte(tc.body)))
		if got != tc.want {
			t.Errorf("%s (%d): %q, want %q", tc.name, tc.status, got, tc.want)
		}
	}
}

// A WITHDRAWN MODEL AND A PAUSED PLAN ARE READ FROM THE FACTS THE REFUSAL DOOR
// STAMPED, not from the prose.
func TestRouteFailureReadsTheStampedFacts(t *testing.T) {
	withdrawn := &APIError{Status: 404, Withdrawn: true}
	if got, _ := RouteFailureOf(withdrawn); got != RouteUnavailable {
		t.Errorf("withdrawn: %q", got)
	}
	paid := &APIError{Status: 403, Payment: true}
	if got, _ := RouteFailureOf(paid); got != RoutePayment {
		t.Errorf("payment fact: %q", got)
	}
	reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	paused := &PlanPauseError{Reset: reset.Format(time.RFC3339), Cause: fmt.Errorf("paused")}
	got, at := RouteFailureOf(paused)
	if got != RouteQuota || !at.Equal(reset) {
		t.Errorf("paused plan: %q until %v, want quota until %v", got, at, reset)
	}
}

// A DEADLINE IS TRANSIENT, A CANCELLATION IS NOBODY'S FAILURE.
func TestRouteFailureOnTheWire(t *testing.T) {
	if got, _ := RouteFailureOf(fmt.Errorf("call: %w", context.DeadlineExceeded)); got != RouteTransient {
		t.Errorf("deadline: %q", got)
	}
	if got, _ := RouteFailureOf(fmt.Errorf("call: %w", context.Canceled)); got != "" {
		t.Errorf("cancelled: %q", got)
	}
	if got, _ := RouteFailureOf(nil); got != "" {
		t.Errorf("nil: %q", got)
	}
}
