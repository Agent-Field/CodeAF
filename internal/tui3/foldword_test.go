package tui3

import (
	"strings"
	"testing"
	"time"
)

// HOME'S FOLD SAYS ITS COUNT AND ITS QUIET ONE WAY.
//
// The fold the phone and the project tails draw ([homeQuietWord]) is how many
// rows are hidden and how long they have been quiet. For a wave the resting list
// had a second writer with a second spelling — `▸ 4 more, quiet since sep 1`
// against `▸ …7 more, quiet since 3h`, a leading ellipsis on one and not the
// other and a calendar date against an elapsed span. That list is retired, and
// [foldWords] and [quietFoldClause] stay the one source of truth.
func TestHomesFoldSpellsItsCountAndItsQuietOneWay(t *testing.T) {
	now := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	tail := homeQuietWord(homeLine{kind: homeQuiet, quiet: 5, since: now.Add(-9 * 24 * time.Hour), folded: true}, now)
	const want = "5 more, quiet since 9d"
	if tail != want {
		t.Fatalf("the project tail's fold drew %q, want %q", tail, want)
	}
	// THE MARK IS THE CALLER'S AND THE ELLIPSIS IS NOBODY'S. A fold that says
	// `▸ …5 more` has drawn two marks for one idea.
	if strings.Contains(tail, "…") {
		t.Fatalf("the fold drew %q, which wears an ellipsis under a fold mark", tail)
	}
	// AND THE AGE IS THE LADDER THE ROWS ABOVE IT WEAR. A month name inside
	// thirty days is the calendar spelling coming back.
	for _, month := range []string{"aug", "sep", "Aug", "Sep"} {
		if strings.Contains(tail, month) {
			t.Fatalf("the fold drew %q, want the elapsed age %q the rows themselves wear", tail, "9d")
		}
	}
}

// AN OPENED FOLD SAYS THE WAY BACK, NOT WHAT IT IS NO LONGER HIDING.
//
// `▾ 5 more` over five rows a person can see leaves the glyph as the only thing
// telling "five are hidden" from "five of these are the ones you asked for".
func TestAnOpenedFoldOnHomeSaysHowManyItWouldTakeAway(t *testing.T) {
	now := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	tail := homeQuietWord(homeLine{kind: homeQuiet, quiet: 5, since: now.Add(-9 * 24 * time.Hour)}, now)
	const want = "5 fewer"
	if tail != want {
		t.Fatalf("the opened project tail drew %q, want %q", tail, want)
	}
}
