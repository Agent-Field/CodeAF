package tui3

// THE ARROWS ARE THE COLUMN'S, AT EVERY WIDTH.
//
// Home's foot says `↑↓ move · enter open` on every frame it draws, and on a
// narrow one it was a promise nothing kept: the roster reads the keyboard
// ABOVE [app.key] (app.go's Update), and its stand-down list named every
// fullscreen page but this one. A roster held on a frame with no column for it
// is drawn nowhere at all while home is up ([app.frame] returns home's frame
// long before [app.railFull] is asked), so the six keys it takes — ↑ ↓ ← → enter
// esc — went into a list nobody could see, and the only thing left that moved
// the selection was the pointer.
//
// These tests press the keys THROUGH [app.Update], which is the whole point:
// calling [app.homeKey] directly is asking home what it would do with a key it
// never received.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// narrowHome opens home on a frame with no second column and no roster column
// either — under [railSlimFloor], which is where a person meets both bugs.
func narrowHome(t *testing.T, lab *homeLab, standing string, width int) *app {
	t.Helper()
	a := lab.app(standing)
	a.width, a.height = width, 30
	a.openHome()
	// The frame settles the tier before a key is read, exactly as a paint does
	// (home.go's [app.homeFrame] rebuilds the column when it crosses
	// [tierPhone]).
	_, _, _, _ = a.homeFrame(a.width, a.height)
	return a
}

// homeCursorKind is the kind of row the cursor is resting on, for the
// assertions that are about WALKING rather than about one row.
func homeCursorKind(a *app) homeRowKind {
	line, ok := a.home.focusedLine()
	if !ok {
		return homeBlank
	}
	return line.kind
}

// homeStops is every row on the column the cursor may rest on.
func homeStops(a *app) []int {
	var out []int
	for at, line := range a.home.lines {
		if line.stop() {
			out = append(out, at)
		}
	}
	return out
}

// twoProjectsAndFive is a machine with more projects than the tier shows, so
// the column has a heading, several conversations, the `elsewhere` rule and the
// folded projects under it — which is a walk with something to step over.
func twoProjectsAndFive(t *testing.T, lab *homeLab) string {
	t.Helper()
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now.Add(-time.Minute))
	lab.session("-tmp-alpha", "aaaa000000000002", "read the law", "/tmp/alpha", now.Add(-2*time.Minute))
	lab.session("-tmp-alpha", "aaaa000000000003", "cut the goldens", "/tmp/alpha", now.Add(-3*time.Minute))
	for i, bucket := range []string{"-tmp-beta", "-tmp-gamma", "-tmp-delta", "-tmp-epsilon", "-tmp-zeta"} {
		lab.session(bucket, "bbbb00000000000"+itoa(i+1), "elsewhere "+itoa(i), "/tmp/other"+itoa(i),
			now.Add(-time.Duration(i+2)*time.Hour))
	}
	return mine
}

// EVERY ROW ON A NARROW COLUMN IS REACHABLE WITH ↑ AND ↓, headings, blanks,
// the `elsewhere` rule and the folded projects under it all stepped over.
func TestArrowsWalkTheWholeColumnOnANarrowHome(t *testing.T) {
	// Sixty-eight is a narrow terminal with no detail column and no roster
	// column; fifty is the phone tier, where the column is an inbox
	// (homephone.go). The keys are the same keys on both.
	for _, width := range []int{68, 50} {
		lab := newHomeLab(t)
		mine := twoProjectsAndFive(t, lab)
		a := narrowHome(t, lab, mine, width)

		stops := homeStops(a)
		if len(stops) < 4 {
			t.Fatalf("at %d columns the column has %d stops, which proves nothing", width, len(stops))
		}
		// ↓ from the top walks every stop there is, in order, and clamps at the
		// bottom rather than wrapping.
		a.home.cursor = stops[0]
		for i, want := range stops[1:] {
			drive(t, a, key("down"))
			if a.home.cursor != want {
				t.Fatalf("at %d columns ↓ number %d landed on line %d, want %d (kind %d)",
					width, i+1, a.home.cursor, want, homeCursorKind(a))
			}
		}
		drive(t, a, key("down"))
		if a.home.cursor != stops[len(stops)-1] {
			t.Fatalf("at %d columns ↓ walked off the bottom of the column", width)
		}
		// And ↑ walks the same stops back up.
		for i := len(stops) - 2; i >= 0; i-- {
			drive(t, a, key("up"))
			if a.home.cursor != stops[i] {
				t.Fatalf("at %d columns ↑ landed on line %d, want %d", width, a.home.cursor, stops[i])
			}
		}
	}
}

// A POINTER RESTING ON A ROW DOES NOT HOLD THE ARROWS DOWN. The hover is a
// preview and the cursor is the selection, and the last input is the one that
// moved (home.go's [app.homeHover]).
func TestAHoveredRowDoesNotStopTheArrowsOnANarrowHome(t *testing.T) {
	lab := newHomeLab(t)
	mine := twoProjectsAndFive(t, lab)
	a := narrowHome(t, lab, mine, 68)

	_, hits, _, _ := a.homeFrame(a.width, a.height)
	var over int
	for y, at := range hits {
		if at > a.home.cursor && a.home.lines[at].stop() {
			over = y
			break
		}
	}
	a.homeHover(1, over)
	if a.home.hover < 0 {
		t.Fatal("the pointer lit no row, so this proves nothing about the arrows")
	}
	was := a.home.cursor
	drive(t, a, key("down"))
	if a.home.cursor == was {
		t.Fatalf("↓ did not move the cursor while a row was hovered (still on line %d)", was)
	}
}

// A ROSTER NOBODY CAN SEE HOLDS NO KEYS. Under [railSlimFloor] the roster is
// raised OVER the body ([app.railFull]) and home is drawn instead of it, so a
// hold left standing from the conversation underneath must not take home's
// arrows, its enter or its esc.
func TestAHeldRosterDoesNotTakeNarrowHomesKeys(t *testing.T) {
	lab := newHomeLab(t)
	mine := twoProjectsAndFive(t, lab)
	a := lab.app(mine)
	a.width, a.height = railSlimFloor-32, 30
	railRun(a)
	a.railTake(true)
	if !a.railHold {
		t.Fatal("the roster did not take the keyboard, so this proves nothing")
	}
	a.openHome()
	_, _, _, _ = a.homeFrame(a.width, a.height)

	was := a.home.cursor
	drive(t, a, key("down"))
	if a.home.cursor == was {
		t.Fatalf("↓ went to a roster nobody can see: the cursor is still on line %d", was)
	}
	drive(t, a, key("up"))
	if a.home.cursor != was {
		t.Fatalf("↑ went to a roster nobody can see: the cursor is on line %d, want %d", a.home.cursor, was)
	}
	// And esc is home's own way out rather than the roster's.
	drive(t, a, key("esc"))
	if a.at(pageHome) {
		t.Fatal("esc handed the keyboard back to a roster instead of closing home")
	}
}

// AND ENTER IS THE PANE'S WHILE THE PANE HOLDS IT. A follow-up typed into a
// stacked `ask here` on a narrow frame is sent by enter, and a held roster must
// not answer it first.
func TestAHeldRosterDoesNotTakeTheFollowUpEnterOnANarrowHome(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine,
		[]session.Event{text(session.EventTextDelta, "I will remind you at 6."), {Kind: session.EventTurnDone}},
		[]session.Event{text(session.EventTextDelta, "and at 7."), {Kind: session.EventTurnDone}},
	)
	a.width, a.height = railSlimFloor-32, 30
	railRun(a)
	a.railTake(true)
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, key("up"), key("enter"))

	ex := theExchange(a)
	if ex == nil {
		t.Fatal("enter did not open the errand, so the follow-up has nowhere to go")
	}
	for _, r := range "and again at 7" {
		drive(t, a, key(string(r)))
	}
	if got := ex.box.String(); got != "and again at 7" {
		t.Fatalf("the follow-up did not reach the pane's box, it holds %q", got)
	}
	drive(t, a, key("enter"))
	if !ex.box.empty() {
		t.Fatalf("enter did not send the follow-up, the box still holds %q", ex.box.String())
	}
	if len(lab.agent.sent) != 2 || lab.agent.sent[1] != "and again at 7" {
		t.Fatalf("the follow-up never left the box, the errand was sent %v", lab.agent.sent)
	}
}
