package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// stripText is the strip as a reader sees it, laid out at the frame's width.
func stripText(a *app) string {
	width, _ := a.size()
	return plain(a.stripRow(width))
}

// THE STRIP IS THE DOOR THAT SURVIVES A NARROW FRAME. The rail is thirty columns
// and the first thing a hundred-column terminal gives up; the strip is one row
// and it is drawn at every width there is a running node, which is the whole
// point — under [railSlimFloor] it is the only thing on screen that leads into
// running work.
func TestTheTaskStripStandsAtEveryWidthWhileWorkRuns(t *testing.T) {
	a, _, _ := taskApp(t)
	if stripText(a) != "" {
		t.Fatalf("an empty session drew a strip:\n%q", stripText(a))
	}

	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(8, "Write the auth tests", session.TaskQueued, session.TaskNotice{})},
	)
	// WIDE: the rail is up as well, and the strip is still the frame's first row
	// — the two answer different questions, and a person watching one node does
	// not want to read a column to find it.
	for _, width := range []int{200, 80} {
		a.width = width
		a.touch()
		if !a.stripShowing() || a.stripHeight() != 1 {
			t.Fatalf("at %d columns nothing raised the strip over a running node", width)
		}
		text := stripText(a)
		for _, want := range []string{"Fix the nil-map", "Write the auth"} {
			if !strings.Contains(text, want) {
				t.Fatalf("at %d columns the strip is missing %q:\n%q", width, want, text)
			}
		}
		// THE CHIP IS A GLYPH AND A NAME: the state's glyph while it runs, the
		// node's own identity cell where the state has nothing to say (taskstrip.go).
		if !strings.Contains(text, plain(a.taskMark(identFor(8)))+" Write the auth") {
			t.Fatalf("at %d columns the queued chip lost its identity cell:\n%q", width, text)
		}
		if w := ansi.StringWidth(text); w > width {
			t.Fatalf("the strip is %d cells wide on a %d-column frame:\n%q", w, width, text)
		}
		// It is the frame's first row, and the body starts under it.
		if got := plain(strings.Split(frame(a), "\n")[0]); !strings.Contains(got, "Fix the nil-map") {
			t.Fatalf("at %d columns the strip is not the frame's first row:\n%q", width, got)
		}
		if a.bodyTop() != 1 {
			t.Fatalf("at %d columns the strip is drawn but not budgeted: top=%d", width, a.bodyTop())
		}
	}

	// AND IT GOES WHEN THE WORK DOES. The roster keeps the record; a permanent
	// row saying nothing is running is a row of chrome bought with a row of
	// conversation.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskDone,
		session.TaskNotice{Merge: mergeWordMerged})})
	if a.stripShowing() || stripText(a) != "" {
		t.Fatalf("the strip outlived the running work:\n%q", stripText(a))
	}
	if a.bodyTop() != 0 {
		t.Fatalf("the strip kept its row after it stopped drawing: top=%d", a.bodyTop())
	}
}

// WHAT THE ROW CANNOT HOLD IT COUNTS. The overflow mark is budgeted for BEFORE
// the chips are laid down — a row that filled itself and then had no room to say
// how many it dropped would be hiding exactly the work a person came looking for
// — and pressing it opens the roster.
func TestTheStripCountsWhatItCannotHoldAndOpensTheRoster(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 44
	for i := 1; i <= 5; i++ {
		a.taskUpdate(update(uint64(i), "node number "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	text := stripText(a)
	if !strings.Contains(text, "+") {
		t.Fatalf("five nodes in forty-four columns dropped none of them:\n%q", text)
	}
	if w := ansi.StringWidth(text); w > a.width {
		t.Fatalf("the strip is %d cells wide on a %d-column frame:\n%q", w, a.width, text)
	}
	if len(a.stripSpans) == 0 || !a.stripMore.pressable() {
		t.Fatalf("the strip recorded no columns to press: chips=%d more=%+v", len(a.stripSpans), a.stripMore)
	}
	if want := stripMoreWord(5 - len(a.stripSpans)); !strings.Contains(text, want) {
		t.Fatalf("the overflow mark does not say %q:\n%q", want, text)
	}

	// THE +N IS THE DOOR TO THE WHOLE ROSTER, which on this frame is the roster
	// over the body (task.go's [app.railFull]).
	drive(t, a, tea.MouseClickMsg{X: a.stripMore.from, Y: 0, Button: tea.MouseLeft})
	if !a.railHold || !a.railFull() {
		t.Fatalf("the overflow mark did not open the roster: hold=%v full=%v", a.railHold, a.railFull())
	}
}

// A CHIP IS A DOOR. Pressing one is the pointer's whole path into a running node
// on a frame with no rail on it.
func TestAStripChipOpensThatNodesRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width = 80 // no rail at all: the strip is the only way in
	a.touch()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Write the auth tests", session.TaskRunning,
		session.TaskNotice{})})
	if a.railShowing() {
		t.Fatal("eighty columns lent the rail its column")
	}

	// The row is laid out first, because laying it out is what records where the
	// chips landed (taskstrip.go's [app.stripPress] says why).
	_ = stripText(a)
	at, found := 0, false
	for _, chip := range a.stripSpans {
		if chip.id == 9 {
			at, found = chip.span.from, true
		}
	}
	if !found {
		t.Fatalf("the second node has no chip on the strip:\n%q", stripText(a))
	}
	drive(t, a, tea.MouseClickMsg{X: at, Y: a.headHeight(), Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 9 {
		t.Fatalf("the chip did not open its node's room: open=%v", a.roomOpen())
	}
	// AND THE ROW SAYS WHICH DOOR YOU WENT THROUGH: the open room's chip wears
	// the accent, which is what makes this a tab row rather than a list. The row
	// itself has moved down one — the room pins its header above it.
	if !strings.Contains(a.stripRow(a.width), sgr256(hueAccent)) {
		t.Fatalf("the open room's chip is not picked out:\n%q", a.stripRow(a.width))
	}
	// A press in the gap after the chips is still the strip's: falling through to
	// the page under it would act on a row the pointer was not over.
	drive(t, a, tea.MouseClickMsg{X: a.width - 1, Y: a.headHeight(), Button: tea.MouseLeft})
	if !a.roomOpen() {
		t.Fatal("a press on the strip's empty end fell through and closed the room")
	}
}

// UNDER THE BREAKPOINT THE ROSTER OPENS OVER THE BODY. Same entries, same folds,
// same footer, same keys — laid out at the width the frame actually has instead
// of squeezed into thirty columns it does not.
func TestTheRosterOpensOverTheBodyOnANarrowFrame(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width = 80
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(8, "Write the auth tests", session.TaskQueued, session.TaskNotice{})},
	)
	if a.railShowing() || a.railFull() {
		t.Fatal("a narrow frame drew a roster nobody asked for")
	}

	drive(t, a, ctrlT())
	if !a.railFull() {
		t.Fatal("ctrl+t did not raise the roster on a frame with no column for it")
	}
	// The strip stands down under it: the overlay is the strip's destination, and
	// an index of the list drawn on top of the list is a row spent twice.
	if a.stripShowing() {
		t.Fatal("the strip drew over the roster it opens")
	}
	lines := strings.Split(plain(frame(a)), "\n")
	if len(lines) != a.height {
		t.Fatalf("the overlay changed the frame's height: %d rows, want %d", len(lines), a.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("overlay row %d is %d cells wide on an %d-column frame:\n%q", i, w, a.width, line)
		}
	}
	body := strings.Join(lines[a.bodyTop():a.bodyTop()+a.viewHeight()], "\n")
	for _, want := range []string{railGroupWords[railRunning], "Fix the nil-map", "Write the auth"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the roster over the body is missing %q:\n%s", want, body)
		}
	}
	// THE SEAM IS A SEAM AND NOT A BORDER, so it is not drawn where there is
	// nothing on the other side of it (task.go's [app.railRows]).
	if strings.Contains(body, railSeam) {
		t.Fatalf("the fullscreen roster drew the rail's seam down its left edge:\n%s", body)
	}
	// The keys are the column's keys, and the pointer's half works the same: a
	// press on a node row is that node's door, wherever on the row it lands.
	if _, ok := a.railEntryAt(a.bodyTop()); !ok {
		t.Fatal("the overlay's first row answers to no entry")
	}

	drive(t, a, key("esc"))
	if a.railFull() || a.railHold {
		t.Fatal("esc did not put the roster away")
	}
	if !a.stripShowing() {
		t.Fatal("the strip did not come back when the roster went away")
	}
}

// THE SPAWN CARD IS A DOOR ONCE THERE IS SOMETHING BEHIND IT. A click used to
// open the brief — the card's own text, one fold down — and the question a person
// has when they press a card about work that has started is what it is DOING.
func TestASpawnCardClickOpensTheNodesRoom(t *testing.T) {
	a, agent, _ := roomApp(t)
	// A frame with no rail on it, so the card is laid out at the width the click
	// is resolved through and the strip is the only other door on screen.
	a.width = 80
	a.touch()
	agent.pending = []uint64{12}
	// Approved with the row's own default, so the card is settled — and running,
	// so there is a node behind it to open.
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 12, 0)}, key("enter"))
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(12, "Write the auth tests", session.TaskRunning,
		session.TaskNotice{})})

	clickHit(t, a, hitTask)
	if !a.roomOpen() || a.room.id != 12 {
		t.Fatalf("a click on the spawn card did not open its node's room: open=%v", a.roomOpen())
	}

	// AND THE BRIEF KEEPS A KEY. ctrl+o is what this surface already means "show
	// me the rest of this" by, and the selected card spends it on the fold the
	// click gave up (task.go's [app.openCard]).
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	drive(t, a, key("up"))
	if a.sel < 0 || a.entries[a.sel].kind != entryTask {
		t.Fatalf("the walk did not reach the spawn card (sel %d)", a.sel)
	}
	open := a.entries[a.sel].card.open
	drive(t, a, key("ctrl+o"))
	if a.entries[a.sel].card.open == open {
		t.Fatal("ctrl+o on the selected card did not open its brief")
	}
}
