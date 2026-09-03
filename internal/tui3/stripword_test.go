package tui3

import (
	"strings"
	"testing"
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
