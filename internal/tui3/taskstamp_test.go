package tui3

// A TIME A PERSON READS IS THE WORK'S OWN, OR THERE IS NO TIME.
//
// An older task replayed out of a checkpoint can carry how long it RAN and
// nothing about when it started — a record from before the stamps existed keeps
// `elapsed_ms` alone. The page used to date one of those by when this WINDOW met
// it plus that duration, which for a terminal opened at 23:52 is a landing time
// after midnight: a stamp in the future. The tasks place then read that as
// tomorrow and dropped the row off the page altogether, so the one piece of work
// waiting on a person went missing while its shorter siblings sat above it
// saying `now`.
//
// These cases pin the three parts of the answer: recorded stamps are preferred,
// a node this window watched keeps its live clock, and an older node without
// either gets silence.

import (
	"strings"
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

// A restored task's own stamps outrank the moment this surface met it and the
// elapsed duration beside them. Reopening the conversation therefore preserves
// both ends of the work's real window.
func TestARestoredTaskReadsTheRecordsOwnStamps(t *testing.T) {
	a, _, _ := taskApp(t)
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	started := now.Add(-35 * time.Minute)
	ended := now.Add(-23 * time.Minute)

	a.taskUpdate(update(13, "Rotate the staging certificate", session.TaskDone, session.TaskNotice{
		Elapsed: 12 * time.Minute, StartedAt: started, EndedAt: ended,
	}))
	node := a.tasks[13]
	if got := node.spawnedAt(); !got.Equal(started) {
		t.Fatalf("the restored task starts at %s, want the recorded %s", got, started)
	}
	if got := taskNodeEnded(node); !got.Equal(ended) {
		t.Fatalf("the restored task lands at %s, want the recorded %s", got, ended)
	}
}

// A completion card reads both ends of its span from the work's record. The
// clock of a conversation reopened later is neither end of yesterday's work.
func TestACompletionCardDrawsTheRecordsOwnSpan(t *testing.T) {
	a, _, _ := taskApp(t)
	reopened := time.Date(2026, time.August, 16, 9, 31, 0, 0, time.UTC)
	a.clock = func() time.Time { return reopened }
	started := time.Date(2026, time.August, 15, 14, 2, 0, 0, time.UTC)
	ended := time.Date(2026, time.August, 15, 14, 14, 0, 0, time.UTC)

	a.taskUpdate(update(14, "Rotate the staging certificate", session.TaskDone, session.TaskNotice{
		Elapsed: 12 * time.Minute, StartedAt: started, EndedAt: ended,
	}))
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the restored task did not draw a completion card")
	}
	card.open = true
	text := taskText(a)
	want := doneSpanLabel + "14:02 → 14:14"
	if !strings.Contains(text, want) {
		t.Fatalf("the completion card does not draw the record's span %q:\n%s", want, text)
	}
	if wrong := doneSpanLabel + "14:02 → 09:31"; strings.Contains(text, wrong) {
		t.Fatalf("the completion card dates yesterday's landing with the reopen clock:\n%s", text)
	}
}

// A completion card whose record carries no wall-clock stamps draws no span
// row. This is the emptiness law: an unknown time is silence, never a guess.
func TestACompletionCardWithNoStampsDrawsNoSpan(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(15, "Rotate the staging certificate", session.TaskDone, session.TaskNotice{
		Elapsed: 12 * time.Minute,
	}))
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the restored task did not draw a completion card")
	}
	card.open = true
	if text := taskText(a); strings.Contains(text, doneSpanLabel) {
		t.Fatalf("a completion card with no recorded stamps draws a span:\n%s", text)
	}
}
