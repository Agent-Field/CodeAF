package tui3

// THE KEY ON A ROW THAT NEEDS YOU GOES WHERE THERE IS SOMETHING TO DO.
//
// A standing item's row on home carries one of two different lines. One is a
// QUESTION the firing put to the person in its own words, and the conversation
// that asked for the item is where that reads — the owner's ruling of
// 2026-09-15, held by TestAWatchAskedForInAnotherProjectStillOpensFromHere.
//
// The other is the line written when a call was refused because nobody was
// there to allow it. Nobody wrote it, nothing in the conversation is waiting on
// it, and a person who pressed the key on such a row landed in an idle room
// with nothing to answer and nowhere to act. These pin the second door and that
// the two are told apart by the line itself.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// The command below is invented for this test. It is not anybody's.
const needsDoorCommand = "go test ./internal/widgets -run TestTheGauge"

func needsDoorItem(t *testing.T, lab *homeLab, line string) (*app, *standBand, standing.Item) {
	t.Helper()
	now := time.Now()
	mine := lab.session("alpha", "s1", "Pricing Research", "/w/alpha", now.Add(-time.Hour))
	asked := lab.session("alpha", "s2", "Standing Up The Watches", "/w/alpha", now.Add(-2*time.Hour))

	watch := bandItem("watch", "keep an eye on that branch", "/w/alpha", standing.WhenProbe, "when it moves")
	watch.NeedsPerson = line
	watch.Updated = now.Add(-time.Minute)
	watch.Created = now.Add(-time.Hour)
	// THE ITEM HAS A CONVERSATION. That is the whole point: with none, this door
	// already went to the item and there was nothing to fix.
	watch.Origin = standing.Origin{SessionID: "s2", Transcript: asked}

	band := &standBand{items: []standing.Item{watch}}
	a := lab.app(mine)
	a.workspace = "/w/alpha"
	band.wire(a)
	a.width, a.height = 180, 40
	openHomeOn(a, mine)
	homeText(a)
	return a, band, watch
}

func TestAnItemStoppedOnAPermissionOpensTheItem(t *testing.T) {
	lab := newHomeLab(t)
	a, _, watch := needsDoorItem(t, lab, standing.NeedsPermissionLead+needsDoorCommand)

	homeLineOf(t, a, func(l homeLine) bool { return l.kind == homeItem })
	drive(t, a, key("enter"))

	if a.at(pageHome) {
		t.Fatalf("enter on an item stopped on a permission stayed on home, saying %q", a.home.msg)
	}
	if !a.at(pageStanding) {
		t.Fatalf("enter opened %q rather than the item's own page", a.file)
	}
	// AND ON THE ITEM ITSELF, not at the top of a list to find it in again.
	if got, ok := a.orders.current(); !ok || got.ID != watch.ID {
		t.Fatalf("enter landed standing on %q (%v), want the item %q", got.ID, ok, watch.ID)
	}
}

// AND THE ROW SAYS THE DOOR IT ACTUALLY TAKES. A word promising the item while
// the key opened a conversation would be worse than the row this replaced.
//
// It is read off the row's own cell rather than off the frame, because the word
// at a row's right is drawn under the CURSOR and this asserts what the row
// says, not where the cursor happens to be standing.
func TestTheStandingRowLeavesTheImplicitEnterHintOut(t *testing.T) {
	lab := newHomeLab(t)
	a, _, _ := needsDoorItem(t, lab, standing.NeedsPermissionLead+needsDoorCommand)

	var said string
	var found bool
	for _, line := range a.home.lines {
		if line.kind == homeItem && line.cell != nil && line.cell.panel == panelNeeds {
			said, found = line.cell.subRight, true
			break
		}
	}
	if !found {
		t.Fatalf("the item did not reach the needs panel at all:\n%s", strings.Join(homeLines(a), "\n"))
	}
	if said != "" {
		t.Fatalf("the row repeats the implicit Enter hint: %q", said)
	}
}

// A QUESTION KEEPS THE DOOR IT HAD. The firing asked it in words, in a
// conversation, and that is where it reads.
func TestAnItemAskingAQuestionStillOpensItsConversation(t *testing.T) {
	lab := newHomeLab(t)
	a, _, _ := needsDoorItem(t, lab, "may I re-run it?")

	homeLineOf(t, a, func(l homeLine) bool { return l.kind == homeItem })
	drive(t, a, key("enter"))

	if a.at(pageStanding) {
		t.Fatal("enter on an item that asked a question opened the item, not the conversation it asked in")
	}
	if a.at(pageHome) {
		t.Fatalf("enter on an item that asked a question stayed on home, saying %q", a.home.msg)
	}
}

// AND THE ROW HIS WATCH ACTUALLY CARRIES TAKES THE ITEM'S DOOR. The line on
// disk today is the engine's own refusal, written by every build before this
// one, and a watch that has spent its allowance for the day cannot fire again
// to have it rewritten. If this row did not move, nothing a person can see
// would have changed.
func TestAnItemStuckInTheOldSpellingOpensTheItem(t *testing.T) {
	lab := newHomeLab(t)
	a, _, watch := needsDoorItem(t, lab, "refused in a task: default — nobody to ask")

	homeLineOf(t, a, func(l homeLine) bool { return l.kind == homeItem })
	drive(t, a, key("enter"))

	if !a.at(pageStanding) {
		t.Fatalf("enter on a row in the old spelling opened %q rather than the item", a.file)
	}
	if got, ok := a.orders.current(); !ok || got.ID != watch.ID {
		t.Fatalf("enter landed standing on %q (%v), want the item %q", got.ID, ok, watch.ID)
	}
}
