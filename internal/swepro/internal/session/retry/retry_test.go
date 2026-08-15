package retry

import (
	"encoding/json"
	"errors"
	"testing"
)

func ptr[T any](value T) *T { return &value }

func TestFacadeDelegatesPureRetryFunctions(t *testing.T) {
	message := "request timed out"
	err := Err{Name: "APIError", Data: ErrData{
		Message: &message, StatusCode: ptr(float64(504)), IsRetryable: ptr(false),
	}}
	if !IsTimeoutError(err) {
		t.Fatal("timeout classifier drifted")
	}
	if got := Delay(1, &err, false); got != TIMEOUT_RETRY_DELAY_DEFAULT {
		t.Fatalf("delay=%v", got)
	}
	if got := Retryable(err, "provider"); got == nil ||
		got.Message != "Provider stalled; failing over to another model" {
		t.Fatalf("retry=%#v", got)
	}
}

func TestPolicyStepParsesSetsAndStops(t *testing.T) {
	var update Decision
	wait, again, err := PolicyStep(1, errors.New("raw"), PolicyOptions{
		Provider: "p",
		Parse: func(any) Err {
			message := "rate limit"
			return Err{Name: "Other", Data: ErrData{Message: &message}}
		},
		Set: func(value Decision) error {
			update = value
			return nil
		},
	})
	if err != nil || !again || wait != RETRY_INITIAL_DELAY ||
		update.Message != "rate limit" || update.Wait != wait {
		t.Fatalf("wait=%v retry=%v update=%#v err=%v", wait, again, update, err)
	}
	_, again, err = PolicyStep(2, nil, PolicyOptions{
		Parse: func(any) Err { return Err{Name: "ContextOverflowError"} },
	})
	if err != nil || again {
		t.Fatalf("retry=%v err=%v", again, err)
	}
}

func TestPolicyStepPropagatesSetFailure(t *testing.T) {
	want := errors.New("set")
	_, again, err := PolicyStep(1, nil, PolicyOptions{
		Parse: func(any) Err {
			message := "too many requests"
			return Err{Name: "Other", Data: ErrData{Message: &message}}
		},
		Set: func(Decision) error { return want },
	})
	if !errors.Is(err, want) || again {
		t.Fatalf("retry=%v err=%v", again, err)
	}
}

func TestFromProviderErrorPreservesRetryHeaders(t *testing.T) {
	body := `{"error":{"message":"slow down"}}`
	err := NewProviderError(
		"Too Many Requests", 429,
		HeaderPairs(map[string][]string{"Retry-After-Ms": {"17"}}), &body,
	)
	classified := FromError(err)
	decision := retryschedStepForTest(classified)
	if decision == nil || decision.Message != "Too Many Requests" || decision.Wait != 17 {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestFromStreamErrorMatchesRetryPolicy(t *testing.T) {
	classified := FromStreamError(json.RawMessage(
		`{"code":"rate_limited","message":"rate limit exceeded"}`,
	))
	info := Retryable(classified, "openrouter")
	if info == nil || info.Message == "" {
		t.Fatalf("stream classification = %#v, retry = %#v", classified, info)
	}
}

func retryschedStepForTest(classified Err) *Decision {
	var decision Decision
	_, again, _ := PolicyStep(1, classified, PolicyOptions{
		Provider: "openrouter",
		Parse:    func(input any) Err { return input.(Err) },
		Set: func(value Decision) error {
			decision = value
			return nil
		},
	})
	if !again {
		return nil
	}
	return &decision
}
