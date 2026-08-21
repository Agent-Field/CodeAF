package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A DOOR THAT PARKED A COMMAND MUST HAVE IT HANDED ON, AND A CLICK IS A DOOR.
//
// [app.press] returns the parked pump for the one press that opens a page
// (harnesscard.go's "change it", which walks into the design's room). The
// keyboard twin of that door — `e`, input.go — already did. The click did not:
// the branch assigned the command to the named result and the function ended in
// `return nil`, so the assignment was dead.
//
// It cost more than the room. [app.openRoom] parks
// `tea.Batch(waitRoom(...), a.wake())`, and [app.wake] sets `a.painting = true`
// as it BUILDS its tick — so dropping that batch left the surface claiming to be
// painting with no frame on the way, and `a.painting` is only ever cleared
// inside [app.paint], which now never runs. Every later `a.wake()` returned nil.
// The frame clock was dead for the rest of the process: no spinners, no
// count-ups, no [app.tickAsk], and streamed text that only appeared when a
// keystroke happened to force a redraw. From in front of it, a session that had
// stopped.
func TestAClickIntoADesignsRoomKeepsTheFrameClockTurning(t *testing.T) {
	a, _, _ := roomApp(t)
	a.height = 44

	// One finished design, pointed at the node the roster is already running, so
	// the middle column of its card has a room to open.
	page := designedPage()
	a.designEvent(session.Event{Kind: session.EventHarnessDesign, ID: 9, Text: "research a topic"})
	a.designEvent(session.Event{Kind: session.EventHarnessDesignDone, ID: 9, Harness: &page})
	card, _ := a.harnessCardOf(9)
	if card == nil {
		t.Fatal("the finished design drew no card")
	}
	card.task = 7

	y := -1
	for at, row := range strings.Split(plain(frame(a)), "\n") {
		if strings.Contains(row, harnessCardChange) {
			y = at
		}
	}
	if y < 0 {
		t.Fatalf("the card drew no actions row:\n%s", plain(frame(a)))
	}
	// The middle column — each answer owns its words and the gap after them
	// (harnesscard.go's [app.harnessCardPress]).
	x := len(harnessCardSave+harnessCardGap) + 1

	// The clock is stood down first, because the roster this fixture is built on
	// already has a node running and is therefore already painting — and a wake
	// that finds the clock up answers nil for a good reason. What is under test
	// is the wake that has real work to do.
	a.painting = false

	// Update rather than drive: the harness swallows the paint clock's own
	// message so that its queue can end, and the whole of this test is whether
	// that message was ever asked for.
	model, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	a = model.(*app)
	if !a.roomOpen() {
		t.Fatal("the click did not walk into the design's room")
	}
	if a.roomPump != nil {
		t.Fatal("the room's pump was never taken off the hook")
	}
	framed := false
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(frameMsg); ok {
			framed = true
		}
	}
	if !framed {
		t.Fatal("the click left the surface claiming to paint with no frame on the way")
	}
	// AND THE CLOCK CAN STILL BE ASKED FOR AFTERWARDS, which is the state the
	// dropped batch actually corrupted: painting true with nothing scheduled is
	// a wake that answers nil forever.
	drive(t, a, frameMsg{})
	if a.wake() == nil && a.painting {
		t.Fatal("the paint clock is wedged: painting, with nothing painting it")
	}
}
