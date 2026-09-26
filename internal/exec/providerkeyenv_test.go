package exec

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
)

// unsetOwned clears both spellings of an owned variable, so a test asserting
// the "off" state cannot pass by accident of whatever the AFORGE_ legacy name
// happens to hold in the runner's own environment.
func unsetOwned(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	t.Setenv(env.Legacy(name), "")
}

// A MODEL'S SHELL MUST NOT INHERIT A PROVIDER CREDENTIAL.
//
// codeaf exports OPENROUTER_API_KEY (or OPENAI_API_KEY, or a vendored
// service's own KeyEnv) into its own process to talk to a provider; a bash
// call the model runs used to inherit that value and could print it straight
// back (issue #1484).
func TestJobShellEnvStripsProviderKeys(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	unsetOwned(t, AllowProviderKeysInShell)
	environment := JobShellEnv([]string{
		"OPENROUTER_API_KEY=sk-or-v1-secret",
		"OPENAI_API_KEY=sk-secret",
		"DEEPSEEK_API_KEY=secret",
		"PATH=/usr/bin",
	})
	for _, name := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY"} {
		if _, ok := envValue(environment, name); ok {
			t.Errorf("%s reached the job shell environment", name)
		}
	}
	if value, ok := envValue(environment, "PATH"); !ok || value != "/usr/bin" {
		t.Errorf("PATH = %q (present %v), want /usr/bin", value, ok)
	}
}

// A CUSTOM KEY VARIABLE IS STILL A PROVIDER CREDENTIAL. A person can name
// their own environment variable for a connected service's key
// (config.PersistedSource.KeyEnv); it must be stripped exactly like a
// conventional one.
func TestJobShellEnvStripsACustomNamedProviderKey(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	unsetOwned(t, AllowProviderKeysInShell)
	if err := config.WriteSources(profile, []config.PersistedSource{
		{ID: "z-ai", Written: "z-ai", KeyEnv: "MY_ZAI_KEY"},
	}); err != nil {
		t.Fatalf("WriteSources: %v", err)
	}

	environment := JobShellEnv([]string{"MY_ZAI_KEY=secret", "PATH=/usr/bin"})
	if _, ok := envValue(environment, "MY_ZAI_KEY"); ok {
		t.Error("MY_ZAI_KEY reached the job shell environment")
	}
}

// THE OPT-IN IS CODEAF'S OWN ENVIRONMENT, NOT THE MODEL'S COMMAND. Whoever
// started codeaf can ask for a task that genuinely needs the running key by
// exporting AllowProviderKeysInShell before codeaf starts; the model cannot
// grant this to itself.
func TestJobShellEnvAllowProviderKeysInShellOptsIn(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv(AllowProviderKeysInShell, "1")
	environment := JobShellEnv([]string{"OPENROUTER_API_KEY=sk-or-v1-secret"})
	if value, ok := envValue(environment, "OPENROUTER_API_KEY"); !ok || value != "sk-or-v1-secret" {
		t.Errorf("OPENROUTER_API_KEY = %q (present %v), want it kept under the opt-in", value, ok)
	}
}

func envValue(env []string, name string) (string, bool) {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}
