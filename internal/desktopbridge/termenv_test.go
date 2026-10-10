package desktopbridge

import (
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestProviderKeyListComesFromTheModelSources(t *testing.T) {
	names := providerKeyEnvNames()
	for _, s := range modelsource.Vendored() {
		if s.KeyEnv != "" && !slices.Contains(names, s.KeyEnv) {
			t.Errorf("%s (%s) is not stripped from the shell", s.KeyEnv, s.ID)
		}
	}
	for _, want := range commonProviderKeyEnv {
		if !slices.Contains(names, want) {
			t.Errorf("%s missing", want)
		}
	}
}

func TestPlainSettingsStillReachTheShell(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-canary")
	t.Setenv("MY_PLAIN_SETTING", "kept")
	got := terminalEnv()
	if slices.Contains(got, "OPENAI_API_KEY=sk-canary") {
		t.Fatal("provider key leaked")
	}
	if !slices.Contains(got, "MY_PLAIN_SETTING=kept") || !slices.Contains(got, "TERM=xterm-256color") {
		t.Fatalf("plain setting or TERM lost: %v", got)
	}
}
