package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE TASK PAGE WHILE IT IS OPEN ──────────────────────────────────────────
//
// The page takes its reading once, on the way in, and re-files it on the beat
// against a stamp. The stamp used to be the reading of the OTHER WINDOWS on this
// machine — which over a connection nothing answers, so it stood still for ever
// and a page opened before the work started never grew its row. A task that has
// not landed exists in no file on any machine; this window's own roster is the
// only authority there is for it.

// pageRows is what the tasks place is drawing, as one string.
func pageRows(t *testing.T, a *app) string {
	t.Helper()
	rows := a.taskSheet.body(a, 120, 40)
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(row.text)
		b.WriteString("\n")
	}
	return b.String()
}

// oneRunningNode is the update a task admitted a moment ago sends: no file
// anywhere has a row for it, and the roster is where it exists.
func oneRunningNode(id uint64, title string) session.Event {
	return session.Event{
		Kind: session.EventTaskUpdate,
		Tool: "propose_task",
		Task: &session.TaskNotice{ID: id, Title: title, State: session.TaskRunning},
	}
}

func TestTheOpenTaskPageGrowsAToldTaskWithoutBeingReopened(t *testing.T) {
	a := hostedPlaceLab(t)
	a.showPage(pageTasks)
	if !a.at(pageTasks) {
		t.Fatal("the tasks place did not open over --host")
	}
	if before := pageRows(t, a); strings.Contains(before, "widening the sluice") {
		t.Fatalf("the row was on the page before the task existed:\n%s", before)
	}

	// The far engine says a node has started, on the standing lane the hosted
	// surface now holds (internal/remote's tasklane.go).
	a.taskUpdate(oneRunningNode(41, "widening the sluice"))

	after := pageRows(t, a)
	if !strings.Contains(after, "widening the sluice") {
		t.Fatalf("the open page never grew the running row:\n%s", after)
	}
}

// AND THE STAMP IS WHAT MAKES IT ONE COMPARISON AND NOT A RE-WALK. A frame that
// changed nothing must leave the reading exactly where it was, or the page would
// re-file itself thirty times a second over a snapshot nobody asked for again.
func TestTheOpenTaskPageDoesNotRefileWhenNothingMoved(t *testing.T) {
	a := hostedPlaceLab(t)
	a.showPage(pageTasks)
	a.taskUpdate(oneRunningNode(41, "widening the sluice"))
	// The first re-file is the one the update earned; the stamp is caught up
	// afterwards, and every frame after that has nothing to do.
	a.taskSheet.regroup(a)

	held := a.taskSheet.mineAt
	a.taskSheet.regroup(a)
	if a.taskSheet.mineAt != held {
		t.Fatalf("a quiet frame moved the stamp from %d to %d", held, a.taskSheet.mineAt)
	}
}

// THE CURSOR STAYS ON THE WORK IT WAS ON WHEN THE LIST MOVES UNDER IT.
//
// A task finishing changes the chronological order inside its conversation.
// Selection must follow the task's identity, rather than staying on a line
// number that now belongs to another task.
func TestTheTaskPageCursorFollowsItsRowWhenAnotherTaskLands(t *testing.T) {
	a := hostedPlaceLab(t)
	a.showPage(pageTasks)
	// Two live rows have distinct activity times, and the newer one is selected.
	first, second := oneRunningNode(41, "widening the sluice"), oneRunningNode(42, "reading the gauge")
	first.Task.StartedAt, second.Task.StartedAt = a.now().Add(-time.Hour), a.now().Add(-2*time.Hour)
	a.taskUpdate(first)
	a.taskUpdate(second)
	a.taskSheet.regroup(a)
	a.taskSheet.cursor = a.tasksSettle(0)
	for i := 0; i < 20; i++ {
		if item, ok := a.taskSheetCurrent(); ok && item.entry.Label == "widening the sluice" {
			break
		}
		a.taskSheetMove(1)
	}
	item, ok := a.taskSheetCurrent()
	if !ok || item.entry.Label != "widening the sluice" {
		t.Fatalf("the walk never reached the second running row: %+v", item.entry)
	}
	was := a.taskSheet.cursor

	// The older sibling finishes and moves ahead of the selected row.
	a.taskUpdate(session.Event{
		Kind: session.EventTaskUpdate,
		Tool: "propose_task",
		Task: &session.TaskNotice{ID: 42, Title: "reading the gauge", State: session.TaskDone, EndedAt: a.now()},
	})
	a.taskSheet.regroup(a)

	now, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatalf("the cursor came off the page entirely:\n%s", pageRows(t, a))
	}
	if now.entry.Label != "widening the sluice" {
		t.Fatalf("the cursor moved from %q to %q while a different task landed:\n%s",
			"widening the sluice", now.entry.Label, pageRows(t, a))
	}
	// AND IT REALLY MOVED, so the assertion above is about the row being followed
	// and not about a list that happened to stay still.
	if a.taskSheet.cursor == was {
		t.Fatal("the fixture did not re-file the list, so nothing was proven")
	}
}
