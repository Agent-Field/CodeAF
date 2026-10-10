package automation

import (
	"errors"
	"testing"
	"time"
)

func openStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func reminder(at time.Time) Automation {
	return Automation{Title: "leave", Schedule: Schedule{At: at}, Action: Action{Say: "time to leave"}, Workspace: "/tmp/project"}
}

func routine(every string) Automation {
	return Automation{Title: "weekly update", Schedule: Schedule{Every: every, Zone: "UTC"}, Action: Action{Do: "draft the weekly update"}, Workspace: "/tmp/project"}
}

func TestStoreRefusesAForeignDatabase(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA application_id=1`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := Open(root); err == nil {
		t.Fatal("a database that is not ours was opened")
	}
}

func TestCreateWorksOutTheFirstWake(t *testing.T) {
	s, now := openStore(t)
	at := now.Add(6 * time.Hour)
	r, err := s.Create(reminder(at))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next.Equal(at) || r.Status != StatusActive || r.Revision != 1 {
		t.Fatalf("created %+v", r)
	}
	w, err := s.Create(routine("15m"))
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(15 * time.Minute); !w.Next.Equal(want) {
		t.Fatalf("a 15m rhythm first wakes %v, want %v", w.Next, want)
	}
	got, err := s.Get(w.ID)
	if err != nil || got.Title != "weekly update" || !got.Schedule.Anchor.Equal(*now) {
		t.Fatalf("read back %+v, %v", got, err)
	}
}

// ONE SLOT, ONE RUN, HOWEVER MANY WERE MISSED. A routine due every fifteen
// minutes that was away for a day catches up once, marked late, and its next
// slot is the next one after now.
func TestTakeCatchesUpOnceAndMarksItLate(t *testing.T) {
	s, now := openStore(t)
	a, err := s.Create(routine("15m"))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(24 * time.Hour)
	due, err := s.Due(*now)
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %v, %v", due, err)
	}
	run, err := s.Take(due[0], *now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Why != WhyLate || !run.Due.Equal(a.Next) {
		t.Fatalf("run %+v", run)
	}
	after, _ := s.Get(a.ID)
	if !after.Next.After(*now) || after.Next.Sub(*now) > 15*time.Minute {
		t.Fatalf("next %v is not the next slot after %v", after.Next, *now)
	}
	if due, _ := s.Due(*now); len(due) != 0 {
		t.Fatalf("still due after the take: %v", due)
	}
	// The same slot cannot be taken twice: a is the copy read before the take,
	// and its slot has moved on.
	if _, err := s.Take(a, *now); err == nil {
		t.Fatal("a slot was taken twice")
	}
}

// THE PERSON'S CHANGE BEATS THE CLOCK'S STALE COPY. A pause made after the clock
// read the automation refuses the clock's take.
func TestTakeIsRefusedWhenThePersonChangedIt(t *testing.T) {
	s, now := openStore(t)
	a, err := s.Create(routine("1h"))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Hour)
	due, _ := s.Due(*now)
	if _, err := s.SetStatus(a.ID, StatusPaused); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Take(due[0], *now); !errors.Is(err, errChanged) {
		t.Fatalf("take after a pause = %v, want errChanged", err)
	}
}

// A ONE-TIME AUTOMATION FINISHES WHEN TAKEN, and a pause is not a missed run:
// a rhythm resumed after a week starts from its next slot, with nothing queued
// for the week.
func TestOneTimeFinishesAndResumeDoesNotCatchUpAPause(t *testing.T) {
	s, now := openStore(t)
	r, err := s.Create(reminder(now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Hour)
	due, _ := s.Due(*now)
	if _, err := s.Take(due[0], *now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(r.ID); got.Status != StatusFinished || !got.Next.IsZero() {
		t.Fatalf("a taken reminder is %+v", got)
	}

	w, _ := s.Create(routine("1h"))
	if _, err := s.SetStatus(w.ID, StatusPaused); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(7 * 24 * time.Hour)
	resumed, err := s.SetStatus(w.ID, StatusActive)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Next.After(*now) {
		t.Fatalf("a resumed rhythm wakes at %v, before now %v", resumed.Next, *now)
	}
	if due, _ := s.Due(*now); len(due) != 0 {
		t.Fatalf("a resumed rhythm caught up its pause: %v", due)
	}
}

func TestUpdateKeepsStateAndRestartsAChangedSchedule(t *testing.T) {
	s, now := openStore(t)
	a, err := s.Create(Automation{
		Title: "ci", Schedule: Schedule{Every: "5m"}, Workspace: "/tmp/p",
		Look:   &Look{Command: "gh run list -L1", Condition: "the latest run failed"},
		Action: Action{Say: "CI is red"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeen(a.ID, a.Revision, "yes", "listing"); err != nil {
		t.Fatal(err)
	}
	a.Title = "CI on main"
	edited, err := s.Update(a)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Seen != "yes" || edited.Memo != "listing" || edited.Revision != 2 || !edited.Next.Equal(a.Next) {
		t.Fatalf("a title edit disturbed state: %+v", edited)
	}
	*now = now.Add(time.Minute)
	edited.Look.Condition = "any run failed"
	edited.Schedule.Every = "10m"
	again, err := s.Update(edited)
	if err != nil {
		t.Fatal(err)
	}
	if again.Seen != "" || again.Memo != "" {
		t.Fatal("a changed condition kept what the old one had seen")
	}
	if want := now.Add(10 * time.Minute); !again.Next.Equal(want) {
		t.Fatalf("a changed rhythm wakes at %v, want %v", again.Next, want)
	}
	// The clock's write with the old revision is refused.
	if err := s.SetSeen(a.ID, a.Revision, "no", ""); !errors.Is(err, errChanged) {
		t.Fatalf("a stale clock write = %v", err)
	}
}

func TestRunsRecordAndChangesReachEveryReader(t *testing.T) {
	s, _ := openStore(t)
	a, _ := s.Create(routine("1h"))
	cursor, err := s.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.QueueNow(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueNow(a.ID); err == nil {
		t.Fatal("a second run was queued while one was waiting")
	}
	if err := s.Start(run.ID); err != nil {
		t.Fatal(err)
	}
	run.Outcome, run.Line, run.USD = OutcomeDone, "drafted it", 0.12
	if err := s.Finish(run); err != nil {
		t.Fatal(err)
	}
	changed, next, err := s.Changes(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].Outcome != OutcomeDone || changed[0].Phase != PhaseOver || next <= cursor {
		t.Fatalf("changes = %+v, cursor %d → %d", changed, cursor, next)
	}
	if more, _, _ := s.Changes(next); len(more) != 0 {
		t.Fatalf("nothing changed, yet %v", more)
	}
	history, _ := s.Runs(a.ID, 10)
	if len(history) != 1 || history[0].Line != "drafted it" || history[0].USD != 0.12 {
		t.Fatalf("history %+v", history)
	}
	// AND THE LIST SAYS WHAT EACH ONE LAST CAME TO, read from the runs and
	// never kept in the document.
	listed, _ := s.List()
	if len(listed) != 1 || listed[0].Last == nil || listed[0].Last.Line != "drafted it" {
		t.Fatalf("the list's last run = %+v", listed)
	}
	got, _ := s.Get(a.ID)
	if got.Last == nil || got.Last.ID != run.ID {
		t.Fatalf("get's last run = %+v", got.Last)
	}
	saved, err := s.Update(got)
	if err != nil || saved.Last != nil {
		t.Fatalf("an update kept a last run in the document: %+v, %v", saved.Last, err)
	}
}

// ONE WINDOW TELLS THE PERSON. Every open window reads the same finished run;
// the first to claim it raises the notification, and claiming it moves nothing
// any window reads, so no window draws the run's line twice.
func TestOneClaimPerRunAndTheClaimIsNotNews(t *testing.T) {
	s, _ := openStore(t)
	a, _ := s.Create(routine("1h"))
	run, _ := s.QueueNow(a.ID)
	_ = s.Start(run.ID)
	run.Outcome = OutcomeDone
	_ = s.Finish(run)
	_, cursor, err := s.Changes(0)
	if err != nil {
		t.Fatal(err)
	}
	if first, err := s.Claim(run.ID); err != nil || !first {
		t.Fatalf("the first claim = %v, %v", first, err)
	}
	if again, err := s.Claim(run.ID); err != nil || again {
		t.Fatalf("a second window claimed the same run: %v, %v", again, err)
	}
	if more, next, _ := s.Changes(cursor); len(more) != 0 || next != cursor {
		t.Fatalf("the claim read as news: %v, cursor %d → %d", more, cursor, next)
	}
}

// A HISTORY THAT ALREADY HAPPENED IS WRITTEN THROUGH THE STORE'S OWN DOORS.
// SetClock is the one thing a fixture adds: every stamp a write makes reads it,
// so a run taken three hours after its slot reads late by exactly that, and nil
// hands every later write back to the wall clock.
func TestSetClockStampsEveryWrite(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clock := time.Date(2026, 9, 28, 10, 12, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return clock })

	a, err := s.Create(routine("1h"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Created.Equal(clock) || !a.Schedule.Anchor.Equal(clock) || !a.Next.Equal(clock.Add(time.Hour)) {
		t.Fatalf("made at %v: created %v, anchor %v, next %v", clock, a.Created, a.Schedule.Anchor, a.Next)
	}

	slot := a.Next
	clock = slot.Add(3 * time.Hour)
	started := clock
	run, err := s.Take(a, started)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(run.ID); err != nil {
		t.Fatal(err)
	}
	clock = started.Add(4 * time.Minute)
	run.Outcome, run.Line = OutcomeDone, "drafted it"
	if err := s.Finish(run); err != nil {
		t.Fatal(err)
	}
	got, err := s.Run(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Due.Equal(slot) || !got.Started.Equal(started) || !got.Finished.Equal(clock) || got.Late() != 3*time.Hour {
		t.Fatalf("the run reads due %v, started %v, finished %v, late %v", got.Due, got.Started, got.Finished, got.Late())
	}

	clock = clock.Add(time.Hour)
	paused, err := s.SetStatus(a.ID, StatusPaused)
	if err != nil {
		t.Fatal(err)
	}
	if !paused.Updated.Equal(clock) {
		t.Fatalf("paused at %v, the change reads %v", clock, paused.Updated)
	}

	s.SetClock(nil)
	before := time.Now().Add(-time.Second)
	asked, err := s.QueueNow(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if asked.Due.Before(before) {
		t.Fatalf("with the wall clock back, a run asked for now is due at %v", asked.Due)
	}
}

func TestAbandonedClosesWhatAGoneClockLeftRunning(t *testing.T) {
	s, _ := openStore(t)
	a, _ := s.Create(routine("1h"))
	run, _ := s.QueueNow(a.ID)
	_ = s.Start(run.ID)
	n, err := s.Abandoned(lineClosed)
	if err != nil || n != 1 {
		t.Fatalf("abandoned %d, %v", n, err)
	}
	got, _ := s.Run(run.ID)
	if got.Phase != PhaseOver || got.Outcome != OutcomeStopped || got.Line != lineClosed {
		t.Fatalf("left %+v", got)
	}
}

func TestDeleteTakesTheHistoryWithIt(t *testing.T) {
	s, _ := openStore(t)
	a, _ := s.Create(routine("1h"))
	_, _ = s.QueueNow(a.ID)
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
	if runs, _ := s.Runs(a.ID, 10); len(runs) != 0 {
		t.Fatalf("history outlived its automation: %v", runs)
	}
	if err := s.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

func TestValidateRefusesWhatCannotRun(t *testing.T) {
	cases := map[string]Automation{
		"no title":         {Schedule: Schedule{Every: "1h"}, Action: Action{Say: "x"}, Workspace: "/p"},
		"no action":        {Title: "t", Schedule: Schedule{Every: "1h"}, Workspace: "/p"},
		"both actions":     {Title: "t", Schedule: Schedule{Every: "1h"}, Action: Action{Say: "x", Do: "y"}, Workspace: "/p"},
		"say in worktree":  {Title: "t", Schedule: Schedule{Every: "1h"}, Action: Action{Say: "x"}, Workspace: "/p", Worktree: true},
		"watch, no rhythm": {Title: "t", Schedule: Schedule{At: time.Now().Add(time.Hour)}, Look: &Look{Command: "x", Condition: "y"}, Action: Action{Say: "x"}, Workspace: "/p"},
		"watch, two looks": {Title: "t", Schedule: Schedule{Every: "5m"}, Look: &Look{Command: "x", Files: "*.go", Condition: "y"}, Action: Action{Say: "x"}, Workspace: "/p"},
		"watch, no cond":   {Title: "t", Schedule: Schedule{Every: "5m"}, Look: &Look{Command: "x"}, Action: Action{Say: "x"}, Workspace: "/p"},
		"no workspace":     {Title: "t", Schedule: Schedule{Every: "1h"}, Action: Action{Say: "x"}},
	}
	for name, a := range cases {
		if err := a.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
