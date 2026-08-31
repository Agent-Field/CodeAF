package tui3

import (
	"strings"
	"testing"
	"time"
)

// THE SWITCHER IS A CARD OVER A DIMMED SURFACE, AND THESE ARE THE FOUR CLAIMS
// THAT MAKE IT ONE (hop.go): what is on it, what the keys do, that nothing
// underneath is left looking live, and that a key which cannot act is neither
// bound nor advertised.

// keepThree puts two more conversations in the keeper and hands back the app
// standing in front of the third. THE NAMES COME OFF THE SIDECAR because a fake
// agent has no title to give, which is also the real fallback: the switcher asks
// the agent, then what the surface was calling it when it was left, and never
// the transcript's file name (hop.go's [hopTitle]).
func keepThree(t *testing.T, a *app) (older, newer *fakeAgent) {
	t.Helper()
	older, newer = &fakeAgent{model: "m"}, &fakeAgent{model: "m"}
	a.stow(Conversation{
		Agent: older, SessionFile: "/tmp/lab/price-scrape.jsonl",
		Workspace: "/tmp/leadgen", Place: "leadgen",
	}, &aside{since: a.now().Add(-3 * time.Hour), title: "openrouter price scrape"})
	a.stow(Conversation{
		Agent: newer, SessionFile: "/tmp/lab/rail-scope.jsonl",
		Workspace: "/tmp/lab", Place: "lab",
	}, &aside{since: a.now().Add(-12 * time.Minute), title: "Refactor the rail scope model"})
	return older, newer
}

// TestTheSwitcherDrawsEveryOpenConversationWithHereLast is the reading, asserted
// row by row: the order, the seam, and where the cursor opens.
func TestTheSwitcherDrawsEveryOpenConversationWithHereLast(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	if !a.hopShowing() {
		t.Fatal("ctrl+k did not raise the switcher")
	}
	rows := a.hop.rows
	if len(rows) != 3 {
		t.Fatalf("the card holds %d rows, and three conversations are open", len(rows))
	}
	// MOST RECENTLY IN FRONT FIRST, which is the order `tab` already walks.
	if !strings.Contains(rows[0].title, "rail scope") {
		t.Fatalf("the first row is %q, and the last conversation was rail-scope", rows[0].title)
	}
	if !strings.Contains(rows[1].title, "price scrape") {
		t.Fatalf("the second row is %q", rows[1].title)
	}
	// AND THE ONE ON SCREEN IS LAST, WITH THE SEAM ON IT.
	if !rows[2].here || rows[2].note != hopHereWord {
		t.Fatalf("the front conversation came out as %+v", rows[2])
	}
	// THE CURSOR OPENS ON THE ROW `tab` WOULD HAVE GONE TO, so the commonest
	// journey through this card is two keys.
	if a.hop.at != 0 {
		t.Fatalf("the cursor opened on row %d", a.hop.at)
	}
	// The project and the age are the two things the tail says about a row it
	// did not have to ask the disk about.
	if rows[1].project != "leadgen" || rows[1].age == "" {
		t.Fatalf("the tail came out as project=%q age=%q", rows[1].project, rows[1].age)
	}
}

// TestTheSwitcherWalksWrapsAndGoes is the keyboard: down, up, round both ends,
// and the one key that actually moves the surface.
func TestTheSwitcherWalksWrapsAndGoes(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	drive(t, a, key("tab"))
	if a.hop.at != 1 {
		t.Fatalf("tab left the cursor on %d", a.hop.at)
	}
	drive(t, a, key("shift+tab"))
	drive(t, a, key("shift+tab"))
	// A RING WRAPS AT BOTH ENDS. Walking up off the top lands on the bottom row
	// rather than stopping there, so an overshoot costs one key and not seven.
	if a.hop.at != len(a.hop.rows)-1 {
		t.Fatalf("walking up off the top left the cursor on %d of %d", a.hop.at, len(a.hop.rows))
	}
	// AND ANOTHER PRESS OF THE OPEN KEY IS ANOTHER STEP DOWN, which is what makes
	// this alt+tab rather than a menu: the gesture is one key, tapped.
	drive(t, a, key(hopOpenKey))
	if a.hop.at != 0 {
		t.Fatalf("a second ctrl+k left the cursor on %d", a.hop.at)
	}

	drive(t, a, key("tab"), key("enter"))
	if a.hopShowing() {
		t.Fatal("enter left the card up")
	}
	if a.file != "/tmp/lab/price-scrape.jsonl" {
		t.Fatalf("enter landed on %q", a.file)
	}
	// AND THE CONVERSATION THAT WAS IN FRONT IS STILL OPEN — a switch is not a
	// close (keeper.go), so the ring is the same size it was.
	if a.openCount() != 3 {
		t.Fatalf("%d conversations are open after the switch", a.openCount())
	}
}

// TestTheSwitcherPutsItselfAwayAndChangesNothing is `esc`: the whole point of a
// layer is that leaving it costs nothing.
func TestTheSwitcherPutsItselfAwayAndChangesNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey), key("tab"), key("esc"))
	if a.hopShowing() {
		t.Fatal("esc left the card up")
	}
	if a.file != "/tmp/lab/this-one.jsonl" {
		t.Fatalf("esc moved the surface to %q", a.file)
	}
	// AND A KEY THAT MEANS NOTHING HERE PUTS IT AWAY WITHOUT REACHING THE DRAFT.
	drive(t, a, key(hopOpenKey), key("z"))
	if a.hopShowing() {
		t.Fatal("an unrelated key left the card up")
	}
	if !a.input.empty() {
		t.Fatalf("the key that closed the card also landed in the box as %q", a.input.String())
	}
}

// TestTheSurfaceUnderTheSwitcherIsDimmed is the depth claim, and it is the whole
// of what says `layer` on a surface that draws no borders (hop.go).
func TestTheSurfaceUnderTheSwitcherIsDimmed(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	if !a.pal.fading() {
		t.Skip("this palette has no depth ladder to fade down")
	}

	body := []string{"one", "two", "three", "four", "five", "six", "seven", "eight",
		"nine", "ten", "eleven", "twelve", "thirteen", "fourteen"}
	drive(t, a, key(hopOpenKey))
	out := a.hopOver(append([]string(nil), body...), 60, a.pal)
	if len(out) != len(body) {
		t.Fatalf("the layer changed the body's height from %d to %d", len(body), len(out))
	}
	dimmed, carded := 0, 0
	for i, line := range out {
		switch {
		case strings.Contains(line, body[i]):
			// The row survived as itself. It may only do that painted.
			if line == body[i] {
				t.Fatalf("row %d is still at full ink under the card: %q", i, line)
			}
			dimmed++
		default:
			carded++
		}
	}
	if dimmed == 0 {
		t.Fatal("nothing behind the card was dimmed")
	}
	if carded < 3 {
		t.Fatalf("the card wrote %d rows over the body", carded)
	}
	// AND THE CARD ITSELF IS NOT DRAWN AT THE FADED STOP. A card a person has to
	// squint at is a card that has taken the keyboard for nothing.
	head := ""
	for _, line := range out {
		if strings.Contains(line, "3 open") {
			head = line
		}
	}
	if head == "" {
		t.Fatalf("the card's head row is not on the frame:\n%s", strings.Join(out, "\n"))
	}
}

// TestASingleConversationHasNoSwitcherAndIsNeverToldAboutOne is the capability
// law: a key that cannot act says so by not being advertised, and the guard and
// the advertisement are one predicate so they cannot come apart.
func TestASingleConversationHasNoSwitcherAndIsNeverToldAboutOne(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	// The legend slot is dropped whole under [hudTight], where the cells are
	// worth more to the conversation's name than to a reminder (render.go).
	a.width = 100

	drive(t, a, key(hopOpenKey))
	if a.hopShowing() || a.hopAvailable() {
		t.Fatal("the switcher opened over the only conversation this terminal holds")
	}
	if strings.Contains(a.legendRight(a.width), hopDoorWord) {
		t.Fatalf("the legend named the switcher with one conversation open: %q", a.legendRight(a.width))
	}

	// WITH TWO OPEN THE SLOT STILL SAYS `tab last`: the card would hold exactly
	// one destination, and `tab` reaches it in one key rather than two.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/other.jsonl"},
		&aside{since: a.now()})
	if got := a.legendRight(a.width); strings.Contains(got, hopDoorWord) {
		t.Fatalf("the legend named the switcher with two open: %q", got)
	}
	// AND WITH THREE IT NAMES THE SWITCHER, because `tab` can now only ever reach
	// one of the two others.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/third.jsonl"},
		&aside{since: a.now()})
	if got := a.legendRight(a.width); !strings.Contains(got, hopDoorWord) {
		t.Fatalf("the legend does not name the switcher with three open: %q", got)
	}
}

// TestTheSwitcherIsFrozenTheMomentItOpens guards the one race a list like this
// has: a conversation finishing a turn stirs the surface, and rows that re-ranked
// on that stir would move the row under the cursor mid-keystroke.
func TestTheSwitcherIsFrozenTheMomentItOpens(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	drive(t, a, key(hopOpenKey))
	first := a.hop.rows[0].title

	// Another conversation arrives in the keeper while the card is up.
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/late.jsonl"},
		&aside{since: a.now()})
	if len(a.hop.rows) != 3 || a.hop.rows[0].title != first {
		t.Fatalf("the card re-read itself under the cursor: %d rows, first %q",
			len(a.hop.rows), a.hop.rows[0].title)
	}
}

// TestTheSwitcherSaysWhatChangedWhileYouWereAway is the note column, which is
// what makes this a catch-up rather than a list of names.
func TestTheSwitcherSaysWhatChangedWhileYouWereAway(t *testing.T) {
	if got := hopNote(true, 4, 2); got != hopAskingWord {
		t.Fatalf("a conversation that wants a person says %q", got)
	}
	if got := hopNote(false, 3, 1); got != "3 tasks running" {
		t.Fatalf("three nodes turning says %q", got)
	}
	if got := hopNote(false, 1, 0); got != "1 task running" {
		t.Fatalf("one node turning says %q", got)
	}
	if got := hopNote(false, 0, 2); got != hopLandedWord {
		t.Fatalf("a turn that ended while away says %q", got)
	}
	if got := hopNote(false, 0, 0); got != hopNothingWord {
		t.Fatalf("a quiet conversation says %q", got)
	}
}

// TestTheAliasIsBoundOnlyWhereTheTerminalCanSpellIt is the capability law again,
// on the one chord that has no legacy encoding (hop.go).
func TestTheAliasIsBoundOnlyWhereTheTerminalCanSpellIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)

	a.keysDisambiguated = false
	if a.hopOpens(hopAlias) {
		t.Fatal("ctrl+tab is bound on a terminal that cannot send it")
	}
	a.keysDisambiguated = true
	if !a.hopOpens(hopAlias) {
		t.Fatal("ctrl+tab is not bound on a terminal that answered the keyboard query")
	}
	drive(t, a, key(hopAlias))
	if !a.hopShowing() {
		t.Fatal("ctrl+tab did not raise the switcher where it can arrive")
	}
}
