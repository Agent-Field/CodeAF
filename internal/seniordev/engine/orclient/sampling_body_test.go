//go:build !windows

package orclient

import (
	"encoding/json"
	"testing"
)

func pointer(value float64) *float64 { return &value }

// TestBuildRequestBodyWritesOnlyTheSamplingFieldsThatAreSet: a nil field is
// absent from the body, not null, so the provider's default applies; a set
// field is written under its OpenRouter key. `provider` is never written by
// the sampling fields — it belongs to the routing block in OpenRouterOptions.
func TestBuildRequestBodyWritesOnlyTheSamplingFieldsThatAreSet(t *testing.T) {
	bare, err := BuildRequestBody(RequestParams{ModelID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(bare, &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"temperature", "top_p", "top_k", "min_p", "seed",
		"frequency_penalty", "presence_penalty", "repetition_penalty", "provider",
	} {
		if _, present := body[key]; present {
			t.Fatalf("unset %s written: %s", key, bare)
		}
	}

	full, err := BuildRequestBody(RequestParams{
		ModelID: "m", MaxOutputTokens: pointer(4096),
		Temperature: pointer(0.2), TopP: pointer(0.9), TopK: pointer(40),
		MinP: pointer(0.05), Seed: pointer(7), FrequencyPenalty: pointer(0.1),
		PresencePenalty: pointer(0.2), RepetitionPenalty: pointer(1.05),
	})
	if err != nil {
		t.Fatal(err)
	}
	body = map[string]any{}
	if err := json.Unmarshal(full, &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body["provider"]; present {
		t.Fatalf("sampling fields wrote provider: %s", full)
	}
	if body["max_tokens"] != 4096.0 || body["temperature"] != 0.2 || body["top_p"] != 0.9 ||
		body["top_k"] != 40.0 || body["min_p"] != 0.05 || body["seed"] != 7.0 ||
		body["frequency_penalty"] != 0.1 || body["presence_penalty"] != 0.2 ||
		body["repetition_penalty"] != 1.05 {
		t.Fatalf("shaped body = %s", full)
	}
}
