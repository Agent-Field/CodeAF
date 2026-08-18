package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestHarnessProgressReplacesOneRowInTheDesignRoom(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 30
	a.tasks = map[uint64]*taskNode{}
	a.tasks[4] = &taskNode{id: 4, title: "design helper"}
	a.room = &taskRoom{id: 4, title: "design helper", unfolded: map[int]bool{}, live: -1, think: -1, dirty: true}
	a.beginHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: "design helper", Task: &session.TaskNotice{ID: 4}})
	a.progressHarnessRoom(session.Event{Kind: session.EventHarnessProgress, ID: 7, Phase: "designing", Attempt: 1, Attempts: 3, Hint: "sketching"})
	a.progressHarnessRoom(session.Event{Kind: session.EventHarnessProgress, ID: 7, Phase: "reviewing", Attempt: 2, Attempts: 3, ThoughtTail: "checking the retry branch"})
	got := roomText(a)
	if strings.Count(got, "harness ·") != 1 || strings.Contains(got, "sketching") || !strings.Contains(got, "reviewing · attempt 2/3 · checking the retry branch") {
		t.Fatalf("room progress did not replace in place:\n%s", got)
	}
	if c, _ := a.harnessCardOf(7); c == nil {
		t.Fatal("the feed card lost its copy")
	}
}

func TestHarnessProgressForUnknownTaskDoesNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.room = &taskRoom{id: 4, unfolded: map[int]bool{}, live: -1, think: -1, dirty: true}
	a.beginHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 7, Task: &session.TaskNotice{ID: 99}})
	a.progressHarnessRoom(session.Event{Kind: session.EventHarnessProgress, ID: 7, Phase: "designing", Hint: "should not land"})
	if a.room.harnessProgress != "" {
		t.Fatalf("unknown task progress reached the room: %q", a.room.harnessProgress)
	}
}
