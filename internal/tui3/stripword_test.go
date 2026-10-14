package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// AND IT SAYS THE STRIP THE WAY THE CARD BESIDE IT SAYS IT.
//
// [homeVerbsWord] is how this surface already advertises a row's verbs, and a
// foot that invented a second grammar for the same strip would be two spellings
// of one idea one edit apart from disagreeing.
func TestAFootNamesTheStripInTheCardsOwnGrammar(t *testing.T) {
	got := homeStripWord(autoVerbPause, autoVerbDelete)
	want := homeVerbsWord + ": " + autoVerbPause + ", " + autoVerbDelete
	if got != want {
		t.Fatalf("the strip clause reads %q, want %q", got, want)
	}
	// AND THE AUTOMATIONS PLACE'S FOOT SAYS IT THAT WAY: one key over one
	// strip, and the verbs behind it listed in that grammar.
	a, _ := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	runCmd(a.openAutomations())
	foot := (placeAutomations{}).hint(a)
	if !strings.Contains(foot, homeVerbsWord+": ") || !strings.Contains(foot, autoVerbPause) {
		t.Fatalf("the automations foot reads %q, want the strip clause with its verbs", foot)
	}
	// A foot with nothing behind the strip says nothing about it, rather than
	// drawing a lead over an empty list.
	if got := homeStripWord(); got != "" {
		t.Fatalf("a row with no verbs drew %q, want nothing", got)
	}
}

// AND A FOOT NEVER OFFERS A VERB OVER A BODY WITH NO ROWS.
//
// The same law from the other end. The test above catches a clause whose KEY is
// only reachable through the `→` strip; this one catches a clause whose key IS
// bound and has nothing to act on — which reads exactly the same way to the
// person who presses it and gets no answer.
//
// On a machine that has run nothing, kept nothing true and remembered nothing,
// the three teaching pages drew:
//
//	tasks     type to filter          (with no rows to filter)
//	standing  enter open where it was asked  (with nothing to open) — the
//	          place automations took the slot of
//	memory    enter open a shelf · alt+s walk the shelves  (with no shelves)
//
// while the body above each of them was spending the whole frame teaching what
// the place is. What is true on a teaching page is the way out, and
// [placeTailed] puts `tab next place` in front of it.
func TestATeachingPagesFootOffersNoVerbOverABodyWithNoRows(t *testing.T) {
	// The whole foot a bare place may draw: the router's clause and the way out,
	// and nothing that acts on a row.
	wayOut := placeHintTail + " · esc"
	for _, page := range []struct {
		what string
		foot func(a *app) string
		// bad is the clauses this page used to promise over nothing.
		bad []string
	}{
		{"tasks", func(a *app) string { return (placeTasks{}).hint(a) },
			[]string{tasksEnterRoomWord, tasksEnterInsideWord,
				tasksEnterAwayWord, tasksEnterOpenWord, tasksEnterJoinWord, tasksClearFilterWord}},
		{"automations", func(a *app) string { return (placeAutomations{}).hint(a) },
			[]string{autoHistoryWord, autoVerbRun, autoVerbPause, autoVerbDelete}},
		{"memory", func(a *app) string { return (placeMemory{}).hint(a) },
			[]string{"open a shelf", "walk the shelves", "type to filter"}},
	} {
		a := newTestApp(nil)
		a.width, a.height = 120, 40
		// A machine with nothing on it: no record, no orders, no memories. Each
		// of the three places reads its own zero value here, which is the state a
		// fresh install is in.
		a.mem.reading = readMemory(store.MemoryShelves{}, nil, "", time.Time{})

		foot := placeTailed(page.foot(a))
		want := wayOut
		if page.what == "tasks" {
			want += " close"
		}
		for _, clause := range page.bad {
			if strings.Contains(foot, clause) {
				t.Errorf("the %s foot on a teaching page reads %q, which offers %q over a body with no rows — want %q",
					page.what, foot, clause, wayOut)
			}
		}
		if foot != want {
			t.Errorf("the %s foot on a teaching page reads %q, want %q — the way out is all that is true there",
				page.what, foot, want)
		}
	}
}
