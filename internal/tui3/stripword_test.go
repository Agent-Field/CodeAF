package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// EVERY CLAUSE ON A FOOT IS ONE KEY AND WHAT IT DOES.
//
// The standing item's foot read `enter open where it was asked · → pause · stop`
// — in which every clause is `key verb` except the last, which is a verb with no
// key at all. A person reads `stop`, presses `s`, and gets nothing: the letters
// are the `→` strip's and only exist once the strip is drawn.
func TestAnItemsFootNeverOffersAVerbWithNoKey(t *testing.T) {
	// The verbs that are ONLY reachable through the strip. A clause that is one
	// of these on its own is the defect: it looks like an offer and is not one.
	stripOnly := map[string]bool{
		homeItemPauseWord: true,
		homeItemStopWord:  true,
		standNotHereWord:  true,
	}
	for _, foot := range []struct{ what, line string }{
		{"the item card's foot", homeItemActions},
		{"the standing place's foot", homeStripWord(homeItemPauseWord, homeItemStopWord, standNotHereWord)},
	} {
		for _, clause := range strings.Split(foot.line, " · ") {
			if stripOnly[strings.TrimSpace(clause)] {
				t.Fatalf("%s reads %q, in which the clause %q is a verb with no key — the letters are the `→` strip's",
					foot.what, foot.line, clause)
			}
		}
	}
}

// AND IT SAYS THE STRIP THE WAY THE CARD BESIDE IT SAYS IT.
//
// [homeVerbsWord] is how this surface already advertises a row's verbs, and a
// foot that invented a second grammar for the same strip would be two spellings
// of one idea one edit apart from disagreeing.
func TestAFootNamesTheStripInTheCardsOwnGrammar(t *testing.T) {
	got := homeStripWord(homeItemPauseWord, homeItemStopWord)
	want := homeVerbsWord + ": " + homeItemPauseWord + ", " + homeItemStopWord
	if got != want {
		t.Fatalf("the strip clause reads %q, want %q", got, want)
	}
	if !strings.HasSuffix(homeItemActions, want) {
		t.Fatalf("the item card's foot reads %q, want it to end with the strip clause %q", homeItemActions, want)
	}
	// AND BOTH VERBS ARE STILL OFFERED — a foot that closed this row by dropping
	// `stop` would have taken a real verb off the screen.
	for _, verb := range []string{homeItemPauseWord, homeItemStopWord} {
		if !strings.Contains(homeItemActions, verb) {
			t.Fatalf("the item card's foot reads %q, which no longer offers %q", homeItemActions, verb)
		}
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
//	standing  enter open where it was asked  (with nothing to open)
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
		{"standing", func(a *app) string { return (placeStanding{}).hint(a) },
			[]string{homeItemEnterWord, homeItemPauseWord, homeItemStopWord, standNotHereWord}},
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
