package bare

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
)

// A provider key in codeaf's own environment must not reach a model's shell.
func TestAProviderKeyDoesNotReachAModelsShell(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("CODEAF_PROFILE_DIR", root)
	t.Setenv("CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL", "")
	t.Setenv(env.Legacy("CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL"), "")
	t.Setenv("OPENROUTER_API_KEY", "probe-not-a-key")
	for _, entry := range StreamingEnv() {
		if strings.HasPrefix(entry, "OPENROUTER_API_KEY=") {
			t.Fatal("OPENROUTER_API_KEY reaches the environment of the model's shell")
		}
	}
}
