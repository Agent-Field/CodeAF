package standing

import (
	"encoding/json"
	"testing"
	"time"
)

func wordsAt(t *testing.T, stamp string) time.Time {
	t.Helper()
	when, err := time.ParseInLocation("2006-01-02 15:04", stamp, time.Local)
	if err != nil {
		t.Fatalf("bad test date %q: %v", stamp, err)
	}
	return when
}

// The line the whole design was written around: a watch that looked, found
// nothing, and did nothing — the one sentence the inbox refuses to carry,
// because a run that came to nothing writes no note anywhere.
func TestAQuietLookSaysItLookedAndFoundNothing(t *testing.T) {
	now := wordsAt(t, "2026-08-25 09:00")
	item := Item{LastChecked: now.Add(-3 * time.Hour), LastCheckLine: "nothing had changed since yesterday"}
	want := "looked 3h ago · nothing had changed since yesterday, so nothing was done"
	if got := LastLookLine(item, now); got != want {
		t.Fatalf("LastLookLine = %q, want %q", got, want)
	}
}

// A check with no words of its own still has to say that it looked and found
// nothing: "nothing" is a finding and the most common one there is.
func TestALookWithNoWordsStillSaysItFoundNothing(t *testing.T) {
	now := wordsAt(t, "2026-08-25 09:00")
	item := Item{LastChecked: now.Add(-90 * time.Minute)}
	want := "looked 1h ago · nothing had changed, so nothing was done"
	if got := LastLookLine(item, now); got != want {
		t.Fatalf("LastLookLine = %q, want %q", got, want)
	}
}

// A firing stamps both instants at once, so a check LATER than the last firing
// is a check that came to nothing — that is the whole discrimination.
func TestAFiringSaysWhatCameOfIt(t *testing.T) {
	now := wordsAt(t, "2026-08-25 09:00")
	fired := now.Add(-3 * time.Hour)
	for outcome, tail := range map[string]string{
		"said":           "it told you",
		"landed":         "it did the work",
		"needs-you":      "it needs your look",
		"failed":         "it could not finish",
		OutcomeNothing:   "it came to nothing",
		"something-else": "",
	} {
		item := Item{
			LastFired: fired, LastChecked: fired,
			LastCheckLine: "the pull request was merged", LastOutcome: outcome,
		}
		want := "fired 3h ago · the pull request was merged"
		if tail != "" {
			want += " — " + tail
		}
		if got := LastLookLine(item, now); got != want {
			t.Fatalf("outcome %q gives %q, want %q", outcome, got, want)
		}
	}
}

// AN ITEM THAT HAS NEVER BEEN LOOKED AT SAYS NOTHING — the emptiness law, and
// the same nothing a rule always says, because nothing ever examines a rule.
func TestAnItemNobodyHasLookedAtSaysNothing(t *testing.T) {
	now := wordsAt(t, "2026-08-25 09:00")
	if got := LastLookLine(Item{}, now); got != "" {
		t.Fatalf("a fresh item says %q, want nothing at all", got)
	}
	rule := Item{When: When{Kind: WhenHold}, Words: "never touch the production database"}
	if got := LastLookLine(rule, now); got != "" {
		t.Fatalf("a rule says %q, want nothing at all", got)
	}
}

// The item's own account of what it saw leads, always: replacing it with a
// phrase of the page's would throw away the only record of what was seen.
func TestTheItemsOwnWordsAreTheOnesThatAreUsed(t *testing.T) {
	now := wordsAt(t, "2026-08-25 09:00")
	item := Item{LastChecked: now.Add(-2 * time.Hour), LastCheckLine: "could not check: the command was not found"}
	want := "looked 2h ago · could not check: the command was not found, so nothing was done"
	if got := LastLookLine(item, now); got != want {
		t.Fatalf("LastLookLine = %q, want %q", got, want)
	}
}

// The ages are the ladder the surface already draws, so that a card and a row
// on one screen are never two clocks.
func TestTheAgeLadderIsTheOneTheSurfaceDraws(t *testing.T) {
	now := wordsAt(t, "2026-08-25 09:00")
	for _, row := range []struct {
		back time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{4 * 24 * time.Hour, "4d ago"},
		{60 * 24 * time.Hour, "on jun 26"},
	} {
		if got := agoWord(now.Add(-row.back), now); got != row.want {
			t.Fatalf("%s ago reads as %q, want %q", row.back, got, row.want)
		}
	}
	if got := agoWord(time.Time{}, now); got != "" {
		t.Fatalf("an instant nobody recorded reads as %q", got)
	}
}

// THREE RUNGS, AND A GRANT IS THE FLOOR — a grant records what an item may do
// WITHOUT asking, so a grant is more rope, not less. Above the floor the count
// of clean firings in a row decides, and the boundaries are what a person
// actually meets: nothing yet, one short, and exactly enough.
func TestTheRopeColumnIsThreeRungsCountedAtItsBoundaries(t *testing.T) {
	granted := func(clean int) Item {
		return Item{Does: Action{Kind: ActionTask},
			Grant: "open a pull request but never merge it", CleanRuns: clean}
	}
	for _, row := range []struct {
		clean int
		want  string
	}{
		{0, "earning trust 0/5"},
		{1, "earning trust 1/5"},
		{TrustAfter - 1, "earning trust 4/5"},
		{TrustAfter, RopeTrusted},
		// A COUNT PAST THE THRESHOLD IS STILL THE TOP RUNG and never "6/5". The
		// fraction is only ever drawn over the range it can honestly print.
		{TrustAfter + 40, RopeTrusted},
	} {
		if got := RopeWord(granted(row.clean)); got != row.want {
			t.Fatalf("a granted item with %d clean firings reads as %q, want %q",
				row.clean, got, row.want)
		}
	}
	// AND NO GRANT IS THE FLOOR WHATEVER THE COUNT SAYS. An item nobody has let
	// act unasked cannot earn its way past that by firing: there is nothing it
	// could do unattended even if it wanted to.
	for _, item := range []Item{
		{},
		{Does: Action{Kind: ActionSay, Say: "the build went red"}},
		{Does: Action{Kind: ActionTask, Brief: "summarise what merged"}, CleanRuns: 99},
		{Grant: "   ", CleanRuns: 99},
	} {
		if got := RopeWord(item); got != RopeAsksFirst {
			t.Fatalf("an item with no grant reads as %q, want %q", got, RopeAsksFirst)
		}
	}
}

// THE COUNT IS KEPT WHERE A FIRING IS RECORDED, and a firing that stopped on a
// question or could not finish puts it back to nothing. Trust is a RUN of clean
// firings and not a tally: an item that needed somebody last night is one
// somebody has to watch again, whatever it did the fortnight before.
func TestAQuestionOrAFailureBreaksTheTrustCount(t *testing.T) {
	item := Item{Grant: "file the invoice", CleanRuns: 4}
	if got := RopeWord(item); got != "earning trust 4/5" {
		t.Fatalf("four clean firings read as %q", got)
	}
	// One more clean firing is the rung; the recorder is what adds it, and this
	// asserts the arithmetic the recorder does (tick.go's fire).
	item.CleanRuns++
	if got := RopeWord(item); got != RopeTrusted {
		t.Fatalf("five clean firings read as %q, want %q", got, RopeTrusted)
	}
	// And a firing that stops on a question takes it to nothing, so the column
	// starts counting again from where the person has to start watching again.
	item.NeedsPerson, item.CleanRuns = "which account should this go against?", 0
	if got := RopeWord(item); got != "earning trust 0/5" {
		t.Fatalf("after a question the rope reads as %q", got)
	}
}

// A STATE IS NOT A RUNG. An item stopped on a question this morning has exactly
// the rope its record earned; the question resets the COUNT, and the column says
// so by counting from nothing rather than by changing what it means. A column
// that flipped between "asks first" and "trusted alone" overnight would be
// answering a different question from the one it is headed with.
func TestBeingStoppedOnAQuestionDoesNotChangeWhichRungIsPossible(t *testing.T) {
	item := Item{Grant: "file the invoice", NeedsPerson: "which account?", CleanRuns: TrustAfter}
	if got := RopeWord(item); got != RopeTrusted {
		t.Fatalf("an item waiting on somebody reads as %q, want %q", got, RopeTrusted)
	}
	if got := RopeWord(Item{NeedsPerson: "which account?"}); got != RopeAsksFirst {
		t.Fatalf("an ungranted item waiting on somebody reads as %q", got)
	}
}

// An exception narrows WHERE an item reaches and not what it may do when it
// gets there.
func TestAnExceptionDoesNotChangeTheRope(t *testing.T) {
	item := Item{Grant: "push to any branch", CleanRuns: TrustAfter,
		Exceptions: []Exception{{Workspace: "/repo/secret"}}}
	if got := RopeWord(item); got != RopeTrusted {
		t.Fatalf("an item with an exception reads as %q, want %q", got, RopeTrusted)
	}
}

// AN ITEM WRITTEN BEFORE THE COUNTER EXISTED DECODES AS ZERO and starts earning
// trust again, which is the conservative direction: nothing on disk says those
// firings were clean, and a column that assumed they were would be granting rope
// nobody measured.
func TestAnOlderItemStartsEarningTrustAgain(t *testing.T) {
	var item Item
	if err := json.Unmarshal([]byte(`{"words":"x","grant":"do the thing","runs":40}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.CleanRuns != 0 {
		t.Fatalf("an item with no cleanRuns key decoded as %d", item.CleanRuns)
	}
	if got := RopeWord(item); got != "earning trust 0/5" {
		t.Fatalf("an item from before the counter reads as %q", got)
	}
}

// A figure a page cannot state at the precision it has is stated in words —
// which is true at every magnitude and cannot be misread as free.
func TestCostPerRunSaysUnderACentRatherThanZero(t *testing.T) {
	if got := CostPerRunWord(Spend{USD: 0.0017 * 3, Fired: 3}); got != "under a cent" {
		t.Fatalf("a sub-cent rate reads as %q", got)
	}
	// Half a cent is the floor and it rounds up to a printable cent.
	if got := CostPerRunWord(Spend{USD: 0.005, Fired: 1}); got != "$0.01" {
		t.Fatalf("half a cent reads as %q", got)
	}
	if got := CostPerRunWord(Spend{USD: 0.62, Fired: 2}); got != "$0.31" {
		t.Fatalf("a measured rate reads as %q, want $0.31", got)
	}
}

// NOTHING FIRED IS NOTHING MEASURED, and so is a run nobody could price. Both
// answer nothing rather than "$0.00", which would read as free.
func TestCostPerRunSaysNothingWhenNobodyMeasuredIt(t *testing.T) {
	for _, spend := range []Spend{
		{},
		{USD: 1.20, Fired: 0},
		{USD: 0, Fired: 14},
	} {
		if got := CostPerRunWord(spend); got != "" {
			t.Fatalf("%+v reads as %q, want nothing at all", spend, got)
		}
	}
}
