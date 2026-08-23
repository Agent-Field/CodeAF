package main

import (
	"os"
	"strings"
	"testing"
)

// stampVersion sets the link-time stamp for one test and puts it back after,
// because it is process-global and every other test in this package reads the
// same variable.
func stampVersion(t *testing.T, value string) {
	t.Helper()
	previous := version
	version = value
	t.Cleanup(func() { version = previous })
}

// An external harness probes this to decide whether aforge is installed at all,
// so the only contract that matters is that it always answers with something
// recognisable — never an empty line, and never a failure.
func TestVersionStringAlwaysAnswers(t *testing.T) {
	answer := versionString()
	if strings.TrimSpace(answer) == "" {
		t.Fatal("versionString() is empty")
	}
	if !strings.HasPrefix(answer, "aforge") {
		t.Fatalf("versionString() = %q, want an aforge-prefixed string", answer)
	}
}

// The release workflow's whole claim is that the tag it cut is the string the
// binary reports. If the stamp did not win, `aforge version` would answer "dev"
// on a tagged release and nobody downstream could tell two builds apart.
func TestVersionStringPrefersTheLinkTimeStamp(t *testing.T) {
	stampVersion(t, "v0.1.0")
	if got, want := versionString(), "aforge v0.1.0"; got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
	stampVersion(t, "build-9b3ff482de3f")
	if got, want := versionString(), "aforge build-9b3ff482de3f"; got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
}

// An unstamped build must still answer. Go writes "(devel)" into build info for
// anything built out of a working tree, and neither that nor the unstamped
// default is a version anyone can look up — both have to read as "dev".
func TestVersionStringFallsBackWhenNothingWasStamped(t *testing.T) {
	for _, placeholder := range []string{"", "   ", "dev", "devel", "(devel)", "UNKNOWN"} {
		stampVersion(t, placeholder)
		answer := versionString()
		if answer != "aforge dev" {
			// A checkout built by `go test` has no usable build info either, so
			// "aforge dev" is the only honest answer here.
			t.Fatalf("versionString() with stamp %q = %q, want %q", placeholder, answer, "aforge dev")
		}
	}
}

// Three spellings, one answer. The agentfield Python doctor runs
// `aforge version`; the Go doctor runs `aforge --version`. If either spelling
// fell through to the unknown-command branch the doctor would report aforge as
// missing on a machine where it is installed and working.
func TestVersionDispatchAcceptsEverySpelling(t *testing.T) {
	stampVersion(t, "v9.9.9")
	previousArgs := os.Args
	t.Cleanup(func() { os.Args = previousArgs })

	for _, spelling := range []string{"version", "--version", "-v"} {
		t.Run(spelling, func(t *testing.T) {
			os.Args = []string{"aforge", spelling}
			printed, err := captureStdout(t, run)
			if err != nil {
				t.Fatalf("aforge %s: %v", spelling, err)
			}
			if strings.TrimSpace(printed) != "aforge v9.9.9" {
				t.Fatalf("aforge %s printed %q, want %q", spelling, printed, "aforge v9.9.9")
			}
		})
	}
}

// The help text is the only place a person finds out the subcommand exists.
func TestUsageMentionsVersion(t *testing.T) {
	if !strings.Contains(usageText, "aforge version") {
		t.Fatal("usageText does not mention `aforge version`")
	}
}
