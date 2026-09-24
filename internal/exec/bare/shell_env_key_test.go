package bare

import (
	"strings"
	"testing"
)

// A provider key in codeaf's own environment must not reach a model's shell.
func TestAProviderKeyDoesNotReachAModelsShell(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "probe-not-a-key")
	for _, entry := range StreamingEnv() {
		if strings.HasPrefix(entry, "OPENROUTER_API_KEY=") {
			t.Fatal("OPENROUTER_API_KEY reaches the environment of the model's shell")
		}
	}
}
