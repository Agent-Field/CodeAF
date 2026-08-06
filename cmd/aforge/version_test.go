package main

import (
	"strings"
	"testing"
)

// An external harness probes this to decide whether aforge is installed at all,
// so the only contract that matters is that it always answers with something
// recognisable — never an empty line, and never a failure.
func TestVersionStringAlwaysAnswers(t *testing.T) {
	version := versionString()
	if strings.TrimSpace(version) == "" {
		t.Fatal("versionString() is empty")
	}
	if !strings.HasPrefix(version, "aforge") {
		t.Fatalf("versionString() = %q, want an aforge-prefixed string", version)
	}
}
