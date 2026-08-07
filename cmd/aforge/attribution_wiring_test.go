package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// Both entry points build their own executor, and a setting honoured at one of
// them is a setting the user cannot trust. The construction sites sit deep
// inside the chat runner and the headless run, past a live provider and a real
// store, so this reads the wiring instead: every place that builds a leaf loop
// has to pass the attribution row into it.
func TestBothExecutorConstructionSitesCarryAttribution(t *testing.T) {
	for _, name := range []string{"chat.go", "run.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if strings.Count(source, "exec.NewLinear(") != 1 {
			t.Fatalf("%s no longer builds exactly one leaf loop", name)
		}
		if !strings.Contains(source, "WithAttribution(") {
			t.Fatalf("%s builds an executor without wiring the attribution setting", name)
		}
	}
}

// A capability aforge has and cannot explain is one the user meets first as a
// surprise in their own git history.
func TestManualExplainsAttribution(t *testing.T) {
	for _, term := range []string{"attribution", "AFORGE_ATTRIBUTION", "sharing", "CONTRIBUTING"} {
		if !manual.Mentions(term) {
			t.Fatalf("no manual page mentions %q", term)
		}
	}
}
