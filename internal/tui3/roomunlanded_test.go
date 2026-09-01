package tui3

// THE OTHER HALF OF THE ROOM'S OWN LAW. roomgone_test.go pins the page a LANDED
// node draws when it has nothing to replay; these pin the page a node that has
// NOT landed draws in the same state. The law room.go states over
// [app.roomRecordRows] — a room is never an empty body under a correct header —
// was enforced for the landed half only, so a queued or running node whose page
// had no blocks yet drew a correct header over a screen with NOTHING on it: the
// worst page this surface can draw, and the one a person reads as the program
// having lost their work.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A NODE STILL WORKING WITH NOTHING ON ITS PAGE SAYS SO, rather than drawing a
// blank. This is the page a person reaches by opening a task the moment they
// start it — the journal is empty because nothing has been written to it yet —
// and the header above it is correct and complete, which is what makes an empty
// body read as a fault.
func TestARunningRoomWithNothingToReplayIsNeverAnEmptyPage(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	if rows := a.roomRows(a.bodyWidth()); len(rows) == 0 {
		t.Fatal("a running node's page drew nothing at all under its header")
	}
	text := roomText(a)
	if !strings.Contains(text, roomYetWord) {
		t.Fatalf("a running node's empty page draws no reason for the blank:\n%s", text)
	}
	// The landed words are both lies here: nothing was lost, and nothing has
	// finished.
	if strings.Contains(text, roomGoneWord) {
		t.Fatalf("a running node's page claims its transcript is gone:\n%s", text)
	}
	if strings.Contains(text, roomFinishedRefusal.what) {
		t.Fatalf("a running node's page says the task finished:\n%s", text)
	}
}

// A QUEUED NODE IS THE SAME PAGE. It is the commonest way to reach this state on
// purpose: work that has not started has journaled nothing, and its room is the
// one a person opens to ask what is happening.
func TestAQueuedRoomWithNothingToReplayIsNeverAnEmptyPage(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskQueued, session.TaskNotice{})})
	clickRail(t, a, 0)

	if rows := a.roomRows(a.bodyWidth()); len(rows) == 0 {
		t.Fatal("a queued node's page drew nothing at all under its header")
	}
	if text := roomText(a); !strings.Contains(text, roomYetWord) {
		t.Fatalf("a queued node's empty page draws no reason for the blank:\n%s", text)
	}
}

// AND THE LINE COMES OFF THE MOMENT THERE IS ANYTHING TO DRAW. It answers one
// question — why is this page empty — so a page that is not empty must not carry
// it, and a running node with a transcript renders exactly as it always did.
func TestARunningRoomWithATranscriptSaysNothingAboutBeingEmpty(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"Reading parseRow now."}`,
	)
	clickRail(t, a, 0)

	text := roomText(a)
	if strings.Contains(text, roomYetWord) {
		t.Fatalf("a page with a transcript on it says it is empty:\n%s", text)
	}
	if !strings.Contains(text, "Reading parseRow now.") {
		t.Fatalf("the transcript did not replay:\n%s", text)
	}
}
