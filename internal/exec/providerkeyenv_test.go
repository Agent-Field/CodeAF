package exec

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
)

// unsetOwned clears both spellings of an owned variable, so a test asserting
// the "off" state cannot pass by accident of whatever the retired-prefix name
// happens to hold in the runner's own environment.
func unsetOwned(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	t.Setenv(env.Legacy(name), "")
}

// isolateProviderKeyHome keeps profile reads and the shell's private tmux
// directory inside this test's throwaway state root.
func isolateProviderKeyHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("CODEAF_PROFILE_DIR", root)
	return root
}

// A MODEL'S SHELL MUST NOT INHERIT A PROVIDER CREDENTIAL.
//
// codeaf exports OPENROUTER_API_KEY (or OPENAI_API_KEY, or a vendored
// service's own KeyEnv) into its own process to talk to a provider; a bash
// call the model runs used to inherit that value and could print it straight
// back (issue #1484).
func TestJobShellEnvStripsProviderKeys(t *testing.T) {
	isolateProviderKeyHome(t)
	unsetOwned(t, "CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL")
	environment := JobShellEnv([]string{
		"OPENROUTER_API_KEY=sk-or-v1-secret",
		"OPENAI_API_KEY=sk-secret",
		"DEEPSEEK_API_KEY=secret",
		"OPENAI_API_BASE=https://local.invalid/v1",
		"HTTPS_PROXY=http://proxy.invalid:8080",
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
	for name, want := range map[string]string{
		"OPENAI_API_BASE": "https://local.invalid/v1",
		"HTTPS_PROXY":     "http://proxy.invalid:8080",
	} {
		if value, ok := envValue(environment, name); !ok || value != want {
			t.Errorf("%s = %q (present %v), want %q", name, value, ok, want)
		}
	}
}

// A CUSTOM KEY VARIABLE IS STILL A PROVIDER CREDENTIAL. A person can name
// their own environment variable for a connected service's key
// (config.PersistedSource.KeyEnv); it must be stripped exactly like a
// conventional one.
func TestJobShellEnvStripsACustomNamedProviderKey(t *testing.T) {
	profile := isolateProviderKeyHome(t)
	unsetOwned(t, "CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL")
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

// A connected service may name an owned variable for its key, whose
// compatibility spelling can also supply the provider credential.
func TestJobShellEnvStripsLegacySpellingOfCustomKey(t *testing.T) {
	profile := isolateProviderKeyHome(t)
	unsetOwned(t, "CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL")
	const keyEnv = "CODEAF_CONNECTED_SERVICE_KEY"
	if err := config.WriteSources(profile, []config.PersistedSource{
		{ID: "z-ai", Written: "z-ai", KeyEnv: keyEnv},
	}); err != nil {
		t.Fatalf("WriteSources: %v", err)
	}
	environment := JobShellEnv([]string{
		keyEnv + "=current-secret", env.Legacy(keyEnv) + "=former-secret",
	})
	for _, name := range []string{keyEnv, env.Legacy(keyEnv)} {
		if _, ok := envValue(environment, name); ok {
			t.Errorf("%s reached the job shell environment", name)
		}
	}
}

// THE OPT-IN IS CODEAF'S OWN ENVIRONMENT, NOT THE MODEL'S COMMAND. Whoever
// started codeaf can ask for a task that genuinely needs the running key by
// exporting AllowProviderKeysInShell before codeaf starts; the model cannot
// grant this to itself.
func TestJobShellEnvAllowProviderKeysInShellOptsIn(t *testing.T) {
	isolateProviderKeyHome(t)
	t.Setenv("CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL", "1")
	environment := JobShellEnv([]string{"OPENROUTER_API_KEY=sk-or-v1-secret"})
	if value, ok := envValue(environment, "OPENROUTER_API_KEY"); !ok || value != "sk-or-v1-secret" {
		t.Errorf("OPENROUTER_API_KEY = %q (present %v), want it kept under the opt-in", value, ok)
	}
}

// A command cannot opt in by adding the setting to the environment handed to
// JobShellEnv; only the process that started codeaf can make that decision.
func TestJobShellEnvIgnoresChildOnlyOptIn(t *testing.T) {
	isolateProviderKeyHome(t)
	unsetOwned(t, "CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL")
	environment := JobShellEnv([]string{
		"CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL=1",
		"OPENROUTER_API_KEY=secret",
	})
	if _, ok := envValue(environment, "OPENROUTER_API_KEY"); ok {
		t.Error("a setting in the child's environment kept the provider key")
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
