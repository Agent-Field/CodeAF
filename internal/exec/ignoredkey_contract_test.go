package exec

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"testing"
)

// C10: The real shell-environment door strips the raw OpenAI credential even
// when the default-provider ladder ignores it.
func TestShellStripsAnIgnoredOpenAIKey(t *testing.T) {
	profile := isolateProviderKeyHome(t)
	unsetOwned(t, "CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL")
	unsetOwned(t, "CODEAF_BASE_URL")
	t.Setenv(config.APIKeyEnv, "")
	const secret = "sk-proj-test-unused-shell-key"
	t.Setenv("OPENAI_API_KEY", secret)
	if config.APIKeyAt(profile) != "" {
		t.Fatal("fixture key must be ignored")
	}
	for _, entry := range JobShellEnv([]string{"OPENAI_API_KEY=" + secret, "PATH=/usr/bin"}) {
		if entry == "OPENAI_API_KEY="+secret {
			t.Fatal("ignored credential reached the model shell")
		}
	}
}
