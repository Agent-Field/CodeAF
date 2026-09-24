package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE FOOT A WAITING QUESTION LEAVES THE ROW'S DOOR STANDING ───────────────
//
// #840 measured a foot where the note had taken the door's place: a task that
// raised a question while its row was selected drew `hello.txt · waiting in
// this conversation · alt+y` on the one line `enter open its room` lived on,
// and the row lost its door. These pin the line that replaced that choice: the
// door FIRST, the note BEHIND it, and the page's own clauses the ones that go
// when the width runs short — never the door, never the note.

// msgFootLine is the last line a place's frame drew, which is the foot: the
// frame ends on the hint row and whatever blank rows follow it.
func msgFootLine(a *app) string {
	last := ""
	for _, line := range strings.Split(placeFrameText(a), "\n") {
		if strings.TrimSpace(line) != "" {
			last = strings.TrimSpace(line)
		}
	}
	return last
}

// TestAWaitingQuestionRidesBehindTheRowDoorOnOneLine is the pair #840 asks for:
// a selected task row whose run raised a question carries the door hint and the
// waiting note on ONE line, and it is the page's own clauses that give way.
func TestAWaitingQuestionRidesBehindTheRowDoorOnOneLine(t *testing.T) {
	a, _ := tasksFootApp(t)
	a.width, a.height = 100, 30
	a.sayWhereQuestionWent(questionWaitingLine(deliveryQuestion(7, session.AskChoice, true)))

	// WIDE, THE DOOR AND THE NOTE ARE BOTH THERE and the page's own clauses
	// have already gone to make room for the note — the verbs clause is the
	// first thing the ladder drops, and it is gone while both phrases stand.
	line := msgFootLine(a)
	if !strings.Contains(line, tasksEnterRoomWord) {
		t.Fatalf("the waiting note took the row's door off the foot:\n%s", placeFrameText(a))
	}
	if !strings.Contains(line, "waiting in this conversation") {
		t.Fatalf("the foot lost the waiting note it was holding:\n%s", placeFrameText(a))
	}
	if !strings.Contains(line, questionChipKey) {
		t.Fatalf("the waiting note arrived without its way in: %q", line)
	}
	if strings.Contains(line, " · · ") {
		t.Fatalf("the note was joined on a doubled separator: %q", line)
	}

	// NARROW, THE WAY OUT GIVES WAY BEFORE THE DOOR DOES. The tail is the last
	// of the between-clauses to leave and the door and the note are still both
	// standing when it has gone, which is the order [hintFitBeside] promises.
	a.width = 80
	line = msgFootLine(a)
	if !strings.Contains(line, tasksEnterRoomWord) || !strings.Contains(line, "waiting in this conversation") {
		t.Fatalf("a narrow foot dropped the door or the note:\n%s", placeFrameText(a))
	}
	if strings.Contains(line, placeHintTail) {
		t.Fatalf("the way out outstayed the row's door: %q", line)
	}
}

// TestANotelessFootKeepsTheDoorHintAsToday is the control: a task that raised
// nothing draws the foot it always drew, unchanged.
func TestANotelessFootKeepsTheDoorHintAsToday(t *testing.T) {
	a, _ := tasksFootApp(t)
	a.width, a.height = 100, 30
	line := msgFootLine(a)
	if line != "enter open its room · tab next place · esc close" {
		t.Fatalf("a foot with nothing to say changed anyway:\n  %q\nwant\n  %q", line, "enter open its room · tab next place · esc close")
	}
	if strings.Contains(line, "waiting in this conversation") {
		t.Fatalf("a task that raised nothing drew a waiting note: %q", line)
	}
}
