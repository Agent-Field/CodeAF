package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A TASK ITS PER-TASK LIMIT ENDED SAYS SO ONCE, UNDER ITS ROW, AND IS LISTED.
// The session ends such a run on the dollar-limit ending, leads its report with
// the limit's sentence and carries the same sentence as the crew's stop
// (session's beltRunNotice). The landing's row draws the sentence exactly once,
// on the line under it, and the Tasks column lists the task as needing the
// person with the sentence beside it — where the limit showed before the tree
// ceiling, when only the crew's guard enforced it.
func TestAPerTaskLimitEndingIsSaidOnceUnderItsRowAndListed(t *testing.T) {
	want := config.CrewTaskCapAction(0.002)
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 120, 40
	crew := &crewroute.Decision{Class: crewroute.Bugfix, Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "deepseek/deepseek-v4-flash", Provider: "openrouter"},
	}}
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 2, Title: "Write three files a.txt, b.txt and c.txt", State: session.TaskRunning, Crew: crew,
	}})
	stopped := *crew
	stopped.Stopped = want
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 2, Title: "Write three files a.txt, b.txt and c.txt", State: session.TaskFailed,
		Ending: session.TaskEndingCostLimit,
		Report: want + "\nWrote a.txt, b.txt and c.txt\nits branch task/write-three-files was kept",
		Result: "Wrote a.txt, b.txt and c.txt", Branch: "task/write-three-files", Merge: "kept",
		Crew: &stopped, EndedAt: time.Now(),
	}})
	card := a.doneCardAt(a.doneEntryFor(2))
	if card == nil || card.hidden() {
		t.Fatalf("the limit's landing drew no row: %+v", card)
	}
	rows := ansi.Strip(strings.Join(a.doneRows(card, a.width, false), "\n"))
	if strings.Count(rows, want) != 1 {
		t.Fatalf("the row says the limit %d times, want once under it:\n%s", strings.Count(rows, want), rows)
	}
	if lines := strings.Split(rows, "\n"); len(lines) < 2 || strings.TrimSpace(lines[1]) != want {
		t.Fatalf("the line under the row is not the limit:\n%s", rows)
	}
	listed := 0
	for _, item := range a.sideBand() {
		if item.key == "task/2" {
			listed++
			if strings.Count(item.words, want) != 1 {
				t.Fatalf("the Tasks column lists the task without the limit once: %q", item.words)
			}
		}
	}
	if listed != 1 {
		t.Fatalf("the Tasks column lists the task %d times, want once", listed)
	}
}
