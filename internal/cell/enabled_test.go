package cell

import (
	"runtime"
	"testing"
)

// TestC26ChannelOverrides proves C26 through the real Enabled door, including
// source builds, invalid values and explicit overrides on every published channel.
func TestC26ChannelOverrides(t *testing.T) {
	t.Cleanup(UseAvailabilityInTests(true))
	previous := releaseRevision
	t.Cleanup(func() { releaseRevision = previous })
	for _, channel := range []struct {
		name, revision string
	}{
		{"source", "5240bb28b"},
		{"unstamped", ""},
		{"dev", "dev-20261021-0123456789ab"},
		{"staging", "staging-20261021-0123456789ab"},
		{"rc", "v0.9.0-rc.1"},
		{"stable", "v0.9.0"},
	} {
		for _, override := range []string{"", "0", "1", "true", "2", " 1"} {
			t.Run(channel.name+"/env="+override, func(t *testing.T) {
				t.Setenv(EnvVar, override)
				releaseRevision = func() string { return channel.revision }
				want := override == "1"
				if runtime.GOOS == "windows" {
					want = false
				}
				if got := Available(); got != want {
					t.Fatalf("Available() = %v, want %v", got, want)
				}
				reason := ""
				if !want {
					reason = OffReason
				}
				if runtime.GOOS == "windows" {
					reason = WindowsReason
				}
				if got := UnavailableReason(); got != reason {
					t.Fatalf("UnavailableReason() = %q, want %q", got, reason)
				}
			})
		}
	}
}
