package enginehost

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
)

// C5: A real Run writes only a private salted digest in its own host directory.
func TestHostEnvironmentFileIsPrivateAndContainsNoValues(t *testing.T) {
	shortHome(t)
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	const key = "sk-or-v1-test-secret-not-in-the-host-file"
	t.Setenv(config.APIKeyEnv, key)
	workspace := "/test/environment-file"
	liveHost(t, workspace)
	dir, err := Dir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, environmentName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("fingerprint mode %v", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(key)) {
		t.Fatal("fingerprint contains clear key")
	}
	if EnvironmentDiffers(workspace) {
		t.Fatal("same environment differs")
	}
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-new")
	if !EnvironmentDiffers(workspace) {
		t.Fatal("changed key matches")
	}
}

// C3 and C4: Unknown fingerprints are silent, and shell bookkeeping does not
// affect a real host's environment comparison.
func TestEnvironmentComparisonIgnoresUnknownFilesAndShellBookkeeping(t *testing.T) {
	shortHome(t)
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	workspace := "/test/environment-unknown"
	liveHost(t, workspace)
	for _, name := range []string{"PWD", "TERM", "SHLVL", "PATH", "TMUX"} {
		t.Setenv(name, "test-irrelevant")
	}
	if EnvironmentDiffers(workspace) {
		t.Fatal("shell bookkeeping differs")
	}
	dir, _ := Dir(workspace)
	path := filepath.Join(dir, environmentName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-changed")
	if EnvironmentDiffers(workspace) {
		t.Fatal("missing fingerprint differs")
	}
	if err := os.WriteFile(path, []byte("incomplete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if EnvironmentDiffers(workspace) {
		t.Fatal("incomplete fingerprint differs")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if EnvironmentDiffers(workspace) {
		t.Fatal("unreadable fingerprint differs")
	}
}

// C3 and C5: Both compatibility spellings yield the same canonical answer,
// while every registered setting and custom provider credential participates.
func TestCanonicalEnvironmentUsesTheRegistryAndSharedProviderList(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := config.WriteSources(profile, []config.PersistedSource{{ID: "z-ai", Written: "z-ai", KeyEnv: "CODEAF_TEST_CONNECTED_KEY"}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(config.ProviderKeyEnvNames(), "CODEAF_BASE_URL") {
		if strings.HasPrefix(name, env.Legacy("CODEAF_")) {
			continue
		}
		t.Setenv(name, "")
		if strings.HasPrefix(name, "CODEAF_") {
			t.Setenv(env.Legacy(name), "")
		}
	}
	for _, row := range config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Rows() {
		if row.Env == "" {
			continue
		}
		t.Setenv(row.Env, "")
		if strings.HasPrefix(row.Env, "CODEAF_") {
			t.Setenv(env.Legacy(row.Env), "")
		}
	}
	if got := canonicalEnvironment(""); got != "" {
		t.Fatalf("empty canonical environment: %q", got)
	}
	for _, name := range append(config.ProviderKeyEnvNames(), "CODEAF_BASE_URL") {
		if strings.HasPrefix(name, env.Legacy("CODEAF_")) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "test-value")
			if got := canonicalEnvironment(""); got != name+"=test-value" {
				t.Fatalf("canonical %q", got)
			}
		})
	}
	for _, row := range config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Rows() {
		if row.Env == "" {
			continue
		}
		t.Run(row.Env, func(t *testing.T) {
			t.Setenv(row.Env, "test-setting")
			if got := canonicalEnvironment(""); got != row.Env+"=test-setting" {
				t.Fatalf("registered setting absent: %q", got)
			}
		})
	}
	t.Setenv("CODEAF_BASE_URL", "test-url")
	current := canonicalEnvironment("")
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(env.Legacy("CODEAF_BASE_URL"), "test-url")
	if got := canonicalEnvironment(""); got != current {
		t.Fatalf("compatibility spelling differs: %q, %q", got, current)
	}
	t.Setenv("CODEAF_TEST_CONNECTED_KEY", "test-key")
	t.Setenv("CODEAF_BASE_URL", "test-url")
	current = canonicalEnvironment("")
	t.Setenv("CODEAF_TEST_CONNECTED_KEY", "")
	t.Setenv(env.Legacy("CODEAF_TEST_CONNECTED_KEY"), "test-key")
	if got := canonicalEnvironment(""); got != current {
		t.Fatal("custom key compatibility spelling differs")
	}
	lines := strings.Split(current, "\n")
	if len(lines) != 2 || lines[0] > lines[1] {
		t.Fatalf("canonical lines not sorted: %q", lines)
	}
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	if strings.Contains(canonicalEnvironment(""), "CODEAF_HOME=") || strings.Contains(canonicalEnvironment(""), config.ProfileDirEnv+"=") {
		t.Fatal("host selectors included")
	}
}

// C3 and F3: Padded absolute and relative profile directories find the same
// provider references as the profile reader, so the daemon's chdir cannot
// create a false environment mismatch.
func TestCanonicalEnvironmentTrimsTheProfileDirectory(t *testing.T) {
	workspace := t.TempDir()
	profile := filepath.Join(workspace, "profile")
	t.Setenv("CODEAF_TEST_PADDED_KEY", "test-padded-key")
	if err := config.WriteSources(profile, []config.PersistedSource{{ID: "z-ai", Written: "z-ai", KeyEnv: "CODEAF_TEST_PADDED_KEY"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.ProfileDirEnv, profile)
	want := canonicalEnvironment(workspace)
	if !strings.Contains(want, "CODEAF_TEST_PADDED_KEY=test-padded-key") {
		t.Fatal("fixture did not read the custom profile key")
	}
	for _, padded := range []string{" \t" + profile + "\t ", " \tprofile\t "} {
		t.Run(padded, func(t *testing.T) {
			t.Setenv(config.ProfileDirEnv, padded)
			if got := canonicalEnvironment(workspace); got != want {
				t.Fatalf("padded profile changed the environment: %q, want %q", got, want)
			}
		})
	}
}
