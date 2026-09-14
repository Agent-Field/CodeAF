package main

import (
	"os"
	"strings"
	"testing"
)

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

// Three spellings, one answer. The agentfield Python doctor runs
// `aforge version`; the Go doctor runs `aforge --version`. If either spelling
// fell through to the unknown-command branch the doctor would report aforge as
// missing on a machine where it is installed and working.
func TestVersionDispatchAcceptsEverySpelling(t *testing.T) {
	previousArgs := os.Args
	t.Cleanup(func() { os.Args = previousArgs })

	for _, spelling := range []string{"version", "--version", "-v"} {
		t.Run(spelling, func(t *testing.T) {
			os.Args = []string{"aforge", spelling}
			printed, err := captureStdout(t, run)
			if err != nil {
				t.Fatalf("aforge %s: %v", spelling, err)
			}
			if strings.TrimSpace(printed) != versionString() {
				t.Fatalf("aforge %s printed %q, want %q", spelling, printed, versionString())
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
