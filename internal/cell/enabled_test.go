package cell

import (
	"runtime"
	"testing"
)

// TestC21ChannelOverrides proves C21 through the real Enabled door, including
// source builds and both explicit overrides on every published channel.
func TestC21ChannelOverrides(t *testing.T) {
	previous := releaseRevision
	t.Cleanup(func() { releaseRevision = previous })
	for _, channel := range []struct {
		name, revision string
		defaultOn      bool
	}{
		{"source", "5240bb28b", true},
		{"unstamped", "", true},
		{"dev", "dev-20261021-0123456789ab", true},
		{"staging", "staging-20261021-0123456789ab", true},
		{"rc", "v0.9.0-rc.1", false},
		{"stable", "v0.9.0", false},
	} {
		for _, override := range []string{"", "0", "1"} {
			t.Run(channel.name+"/env="+override, func(t *testing.T) {
				t.Setenv(EnvVar, override)
				releaseRevision = func() string { return channel.revision }
				want := channel.defaultOn
				if override != "" {
					want = override == "1"
				}
				if runtime.GOOS == "windows" {
					want = false
				}
				if got := Enabled(); got != want {
					t.Fatalf("Enabled() = %v, want %v", got, want)
				}
				if !want && DisabledReason() == "" {
					t.Fatal("disabled cells have no explanation")
				}
			})
		}
	}
}
