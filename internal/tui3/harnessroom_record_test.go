package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A design cancelled before its first milestone has progress but no transcript
// blocks. designSeat.broke deliberately does not publish context cancellation;
// landHarnessNode records the ending and the task notice carries that report,
// without an EventHarnessDesignDone. Closing the live lane must therefore draw
// the durable report rather than let the stale progress suppress an empty page.
func TestAStoppedDesignRoomKeepsItsRecordedEnding(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 30
	const title = "design the import helper"
	const report = "The design was stopped before it finished; nothing was saved."
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, title, session.TaskRunning,
		session.TaskNotice{Brief: "Build an import helper."})})
	a.room = a.newRoom(4, title)
	a.designEvent(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: title,
		Task: &session.TaskNotice{ID: 4}})
	a.designEvent(session.Event{Kind: session.EventHarnessProgress, ID: 7,
		Phase: "designing", Hint: "receiving the draft"})
	if before := roomText(a); !strings.Contains(before, "receiving the draft") {
		t.Fatalf("the design progress never reached its room:\n%s", before)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, title, session.TaskFailed,
		session.TaskNotice{Report: report, Stopped: true})})
	drive(t, a, roomClosedMsg{gen: a.room.gen})
	if !a.room.done || a.tasks[4].report != report {
		t.Fatal("the stopped task did not reach its terminal state with its report")
	}
	got := roomText(a)
	if strings.Contains(got, "receiving the draft") {
		t.Fatalf("the finished page kept claiming design activity:\n%s", got)
	}
	if !strings.Contains(got, report) {
		t.Fatalf("stale design progress hid the recorded ending:\n%s", got)
	}
}
