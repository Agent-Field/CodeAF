package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
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

// TestHarnessRoomRowPrefersTheHintOverTheThinking: the room streams the
// designer's reasoning in full already, so when the progress event carries both
// halves of the stream, this row shows the JSON half — the hint — rather than
// repeating a line of the prose scrolling above it.
func TestHarnessRoomRowPrefersTheHintOverTheThinking(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 30
	a.tasks = map[uint64]*taskNode{}
	a.tasks[4] = &taskNode{id: 4, title: "design helper"}
	a.room = &taskRoom{id: 4, title: "design helper", unfolded: map[int]bool{}, live: -1, think: -1, dirty: true}
	a.beginHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: "design helper", Task: &session.TaskNotice{ID: 4}})
	a.progressHarnessRoom(session.Event{Kind: session.EventHarnessProgress, ID: 7, Phase: "designing", Attempt: 1, Attempts: 3,
		ThoughtTail: "let me reconsider the split", Hint: "4 steps so far"})
	got := roomText(a)
	if !strings.Contains(got, "designing · attempt 1/3 · 4 steps so far") || strings.Contains(got, "reconsider") {
		t.Fatalf("the room row repeated the thinking instead of showing the hint:\n%s", got)
	}
}

// TestTheRoomTickerGoesAwayWhenThePageLands: no further progress event is
// coming once the design is awaiting the person's look, so a ticker left in
// place kept saying "reviewing" under a finished page forever.
func TestTheRoomTickerGoesAwayWhenThePageLands(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 30
	a.tasks = map[uint64]*taskNode{}
	a.tasks[4] = &taskNode{id: 4, title: "design helper"}
	a.room = &taskRoom{id: 4, title: "design helper", unfolded: map[int]bool{}, live: -1, think: -1, dirty: true}
	a.beginHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 7, Text: "design helper", Task: &session.TaskNotice{ID: 4}})
	a.progressHarnessRoom(session.Event{Kind: session.EventHarnessProgress, ID: 7, Phase: "reviewing", Hint: "checking the draft"})
	a.finishHarnessCard(session.Event{Kind: session.EventHarnessDesign, ID: 7, Harness: &subharness.Harness{}})
	if a.room.harnessProgress != "" {
		t.Fatalf("the ticker outlived the design: %q", a.room.harnessProgress)
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
