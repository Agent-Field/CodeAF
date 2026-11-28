package config

import "testing"

// AN OLLAMA TAG IS NOT A THINKING LEVEL: `ollama/flint:latest` is the model's
// own name, and only a colon after the tag can carry a level.
func TestTierValueAcceptsOllamaModelTags(t *testing.T) {
	for _, value := range []string{
		"ollama/flint:latest",
		"Ollama/qwen3:0.6b",
		"ollama/qwen3:0.6b:high",
		"ollama/hf.co/owner/model-GGUF:Q4_K_M",
	} {
		if err := ValidateTierValue(value); err != nil {
			t.Errorf("ValidateTierValue(%q) = %v, want nil", value, err)
		}
		if _, _, err := ParseCrewPin(value); err != nil {
			t.Errorf("ParseCrewPin(%q) = %v, want nil", value, err)
		}
	}
	for _, value := range []string{"ollama/qwen3:0.6b:hgih", "vendor/model:latest", "vendor/ollama/model:hgih"} {
		if err := ValidateTierValue(value); err == nil {
			t.Errorf("ValidateTierValue(%q) = nil, want a refusal", value)
		}
	}
	dir := t.TempDir()
	if err := writeTierModel(dir, ModelTierReflex, "ollama/flint:latest"); err != nil {
		t.Fatalf("writing the reflex row: %v", err)
	}
	if got := TierModelAt(dir, ModelTierReflex); got != "ollama/flint:latest" {
		t.Fatalf("reflex row = %q, want ollama/flint:latest", got)
	}
}
