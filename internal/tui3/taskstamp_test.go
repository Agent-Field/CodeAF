package tui3

// A TIME A PERSON READS IS THE WORK'S OWN, OR THERE IS NO TIME.
//
// A task replayed out of a checkpoint carries how long it RAN and nothing about
// when it started — the record keeps `elapsed_ms` and no stamp. The page used to
// date one of those by when this WINDOW met it plus that duration, which for a
// terminal opened at 23:52 is a landing time after midnight: a stamp in the
// future. The tasks place then read that as tomorrow and dropped the row off the
// page altogether, so the one piece of work waiting on a person went missing
// while its shorter siblings sat above it saying `now`.
//
// These cases pin the two halves of the answer: a node with a real start still
// gets a real landing time, and a node without one gets silence.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// TestARestoredTaskIsNeverDatedInTheFuture is the defect itself, reproduced at
// the hour that made it visible.
func TestARestoredTaskIsNeverDatedInTheFuture(t *testing.T) {
	a, _, _ := taskApp(t)
	// EIGHT MINUTES TO MIDNIGHT, which is when the audit's frames were taken and
	// is the whole reason this was findable: at noon a stamp twelve minutes into
	// the future is merely wrong, and at 23:52 it is on tomorrow's page.
	night := time.Date(2026, time.August, 15, 23, 52, 0, 0, time.UTC)
	a.clock = func() time.Time { return night }

	// A conversation being reopened: the graph replays a node that is ALREADY
	// settled, carrying the twelve minutes it ran and no start.
	const label = "Cut every list on the task surface over to the shared row fitter"
	a.taskUpdate(update(11, label, session.TaskUnverified, session.TaskNotice{Elapsed: 12 * time.Minute}))

	node := a.tasks[11]
	if node == nil {
		t.Fatal("the replayed node never reached the roster at all")
	}
	if at := taskNodeEnded(node); !at.IsZero() {
		t.Fatalf("a task restored at %s is dated %s — %s in the future, which is a landing time nobody can have; a node whose start nobody knows must be dated nothing",
			night.Format("15:04"), at.Format("Jan 2 15:04"), at.Sub(night))
	}
	if at := node.spawnedAt(); !at.IsZero() {
		t.Fatalf("a restored task says it started at %s, which is when this terminal opened and not when the work ran; it should say nothing",
			at.Format("15:04"))
	}

	// AND THE ROW IS STILL ON THE PAGE. Answering the zero time is only half the
	// fix: the date window cannot judge a row with no stamp on it, so an undated
	// row is kept rather than thrown away — otherwise the emptiness law would
	// have taken the row off the page in a different way.
	var row *session.TaskIndexEntry
	rows := a.taskSheetOwnRows()
	for i := range rows {
		if rows[i].ID == "11" {
			row = &rows[i]
		}
	}
	if row == nil {
		t.Fatalf("the restored task is not among the %d rows this window offers the tasks place", len(rows))
	}
	if !row.EndedAt.IsZero() {
		t.Fatalf("the row handed to the tasks place is dated %s, want no date at all", row.EndedAt.Format("Jan 2 15:04"))
	}

	win := session.LastDays(a.now(), taskSheetDays)
	reading := readTasks(session.World{}, a.taskSheetMine(), win, time.Time{}, a.now())
	on := false
	for _, item := range reading.items {
		if item.entry.ID == "11" {
			on = true
			if age := tasksAgeField(item, a.now()).full; age != "" {
				t.Fatalf("the restored task is aged %q on the page, and nothing anywhere records when it landed", age)
			}
		}
	}
	if !on {
		t.Fatalf("the task that needs a person is not on the tasks place at all: the window %q holds %d rows",
			win.Label(), len(reading.items))
	}
}

// TestAWatchedTaskKeepsItsOwnLandingTime is the other half. Silence is for work
// nobody timed; work this window watched from the start has a real clock and
// must still be dated by it.
func TestAWatchedTaskKeepsItsOwnLandingTime(t *testing.T) {
	a, _, tick := taskApp(t)

	a.taskUpdate(update(12, "Fold the settled work", session.TaskRunning, session.TaskNotice{}))
	began := a.tasks[12].began
	if began.IsZero() {
		t.Fatal("a node this window watched start has no start on it")
	}
	tick(4 * time.Minute)
	a.taskUpdate(update(12, "Fold the settled work", session.TaskDone, session.TaskNotice{Elapsed: 4 * time.Minute}))

	want := began.Add(4 * time.Minute)
	if got := taskNodeEnded(a.tasks[12]); !got.Equal(want) {
		t.Fatalf("a node this window watched landed at %s, want %s — its own start plus its own run",
			got.Format("15:04:05"), want.Format("15:04:05"))
	}
	if got := a.tasks[12].spawnedAt(); !got.Equal(began) {
		t.Fatalf("a watched node says it started at %s, want %s", got.Format("15:04:05"), began.Format("15:04:05"))
	}
}
