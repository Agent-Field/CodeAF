package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// Both entry points build their own executor, and a setting honoured at one of
// them is a setting the user cannot trust. The construction sites sit deep
// inside the resident dispatch and the headless run, past a live provider and a
// real store, so this reads the wiring instead: every place that builds a leaf
// loop has to pass the attribution row into it.
//
// The resident's site is the constructor table now rather than chat.go — a
// worker is chosen per node, so the choice and the construction moved together —
// and chat.go builds no loop of its own at all. That last part is the invariant
// worth keeping: a second construction site growing back inside the dispatch
// path is exactly how one of these settings gets honoured in one place and not
// the other.
func TestBothExecutorConstructionSitesCarryAttribution(t *testing.T) {
	for _, name := range []string{"subharness.go", "run.go"} {
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
	raw, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "exec.NewLinear(") {
		t.Fatal("chat.go builds a leaf loop of its own again instead of asking the table for one")
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
