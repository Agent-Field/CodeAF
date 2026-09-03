package tui3

// ── THE LANDED CARD'S OWN WORDS ─────────────────────────────────────────────
//
// The card is the END of something a person delegated ten minutes ago and the
// only place the outcome is ever stated, so the two things it can quietly get
// wrong are its punctuation and its vocabulary. These pin both.

import (
	"strings"
	"testing"
	"time"
)

// ONE SEPARATOR MEANS ONE THING ON THE ROW. The span used to be joined to the
// state word with a bare space while every other fact on the same row was joined
// with ` · `, so `needs your look 12m00s` read as one phrase — the state word,
// which is the reason the card is asking for a hand at all, fused into a
// duration — beside `4 files · merged`, which was properly separated.
func TestALandedCardSeparatesItsStateWordFromItsClock(t *testing.T) {
	a, _, _ := taskApp(t)
	card := &taskDone{
		title: roomLongName, unverified: true,
		span: 12 * time.Minute, changed: []string{"a", "b", "c", "d"},
		merge: mergeWordMerged,
	}
	tail := plain(a.doneTail(card))
	want := " · " + taskUnverifiedWord + " · " + taskSpanWord(12*time.Minute)
	if !strings.Contains(tail, want) {
		t.Fatalf("the card's head reads\n\t%q\nand its state word and its clock should be two facts:\n\t%q", tail, want)
	}
	if strings.Contains(tail, taskUnverifiedWord+" 12m00s") {
		t.Fatalf("the card still fuses the state word into the duration:\n\t%q", tail)
	}
	// AND EVERY OTHER JOIN ON THE ROW IS THE SAME ONE, so the single separator
	// on this row means exactly one thing.
	for _, want := range []string{" · 4 files", " · " + mergeScreenWord(mergeWordMerged)} {
		if !strings.Contains(tail, want) {
			t.Fatalf("the card's head is missing %q:\n\t%q", want, tail)
		}
	}
}

// THE WORD FOR WHEN THE WORK BEGAN IS `started`, and never `spawned`. That is
// the machinery's own verb for launching a process, which this house bans in
// anything a person reads — the same rule that took `worktree` off the branch
// row of this very file on 2026-09-01.
func TestALandedCardSaysStartedAndNeverSpawned(t *testing.T) {
	a, _, _ := taskApp(t)
	began := a.now().Add(-20 * time.Minute)
	card := &taskDone{title: "Fix the nil-map crash", outcome: "the guard is in", started: began}
	under := plain(a.doneUnder(card, 120))
	if !strings.Contains(under, "started "+began.Format("15:04")) {
		t.Fatalf("the card's second row reads\n\t%q\nand it should say %q", under, "started "+began.Format("15:04"))
	}
	if strings.Contains(strings.ToLower(under), "spawned") {
		t.Fatalf("the card's second row still says `spawned`, which is the machinery's own verb:\n\t%q", under)
	}
	// AND A CARD WITH NO START DRAWS NO STAMP. A task replayed out of a
	// checkpoint keeps how long it ran and not when it began, and the emptiness
	// law draws nothing rather than a time this surface invented.
	bare := plain(a.doneUnder(&taskDone{title: "Fix the nil-map crash", outcome: "the guard is in"}, 120))
	if strings.Contains(bare, "started") {
		t.Fatalf("a card that knows no start stamped one anyway:\n\t%q", bare)
	}
}
