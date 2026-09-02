package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/trace"
)

// ── /debug BELONGS TO THE CONVERSATION IT WAS TYPED IN ──────────────────────
//
// One process can hold several conversations — an engine host holds one per
// person sitting in front of it — and the record holds their prompts, their
// files and the model's whole reply. So a /debug typed in one of them must turn
// the record on for THAT run and no other; a switch that could not tell them
// apart would land somebody's own data in a folder they never asked for.
//
// The three phases are one test on purpose: the last of them turns the
// PROCESS-wide switch on, which is deliberately one-way, so it cannot run
// before the two that need it off.
func TestDebugRecordsOnlyTheConversationItWasTypedIn(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	mine := newTestApp(&fakeAgent{model: "m"})
	mine.ctx = trace.WithRun(mine.ctx, "aaaa1111")
	theirs := newTestApp(&fakeAgent{model: "m"})
	theirs.ctx = trace.WithRun(theirs.ctx, "bbbb2222")

	// Typing it says what it did and where the record goes, naming ITS run.
	mine.slash("/debug")
	folder := trace.Dir("aaaa1111")
	if got, want := plain(lastNote(t, mine)), "recording this conversation · it goes to "+folder; got != want {
		t.Fatalf("/debug said %q, want %q", got, want)
	}
	if !trace.EnabledRun(mine.ctx) {
		t.Fatalf("/debug did not switch its own conversation on")
	}

	// THE SECOND CONVERSATION IS UNTOUCHED — the defect this test exists for.
	if trace.EnabledRun(theirs.ctx) {
		t.Fatalf("/debug in one conversation switched another one on")
	}
	if trace.Enabled() {
		t.Fatalf("/debug flipped the switch for the whole process")
	}

	// Typing it twice says it is already on, and where, rather than nothing.
	mine.slash("/debug")
	if got, want := plain(lastNote(t, mine)), "the record is already on · it goes to "+folder; got != want {
		t.Fatalf("a second /debug said %q, want %q", got, want)
	}

	// And where the pin or the flag already turned the whole process on, it
	// says THAT instead: "on for everything this aforge is doing" is a
	// different fact from "on for you", and a person reading the folder later
	// needs to know which.
	trace.Enable()
	theirs.slash("/debug")
	got := plain(lastNote(t, theirs))
	if !strings.HasPrefix(got, "the record is already on for every conversation this aforge holds · this one goes to ") {
		t.Fatalf("/debug under the process switch said %q", got)
	}
	if !strings.HasSuffix(got, trace.Dir("bbbb2222")) {
		t.Fatalf("/debug named %q rather than this conversation's own folder", got)
	}
}

// A surface that never began a run has nothing to record, and says so — the
// alternative is a command that reports success and writes nowhere.
func TestDebugSaysSoWhenThereIsNoRunToRecord(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	a := newTestApp(&fakeAgent{model: "m"})
	// The run id the app would carry is stripped back to nothing; on a process
	// where a door has begun a run, [trace.RunFrom]'s fallback still names it,
	// so this asserts only the shape of the sentence when there is none.
	if trace.RunFrom(a.ctx) != "" {
		t.Skip("a door in this process has begun a run, so there is one to fall back to")
	}
	a.slash("/debug")
	if got, want := plain(lastNote(t, a)), "this conversation has no run to record."; got != want {
		t.Fatalf("/debug said %q, want %q", got, want)
	}
}
