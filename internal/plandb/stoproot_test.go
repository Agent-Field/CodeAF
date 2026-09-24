package plandb

import (
	"path/filepath"
	"testing"
	"time"
)

// A PERSON'S STOP ENDS THE WHOLE RUN IN THE STORE, AND KEEPS WHAT HAD LANDED.
// The run's own task was the one task nothing could end from outside: every
// verb refused it, so a run a person stopped stayed open in the store and the
// next hand-off in the same conversation adopted it and picked the stopped work
// back up. The runtime's verb for the person's word cancels the run's own task
// and everything still open under it, at any depth, and
// leaves every task that had already ended exactly as it ended.
func TestStopRootEndsTheRunAndEverythingStillOpenUnderIt(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, store, planSpec("landed", "Landed"), planSpec("going", "Going"), planSpec("waiting", "Waiting"))
	planFinish(t, store, "landed", "worker", "landed delivered")
	// WORK TWO LEVELS DOWN is still the run's work.
	child := planSpec("late", "Late")
	child.ParentID = "waiting"
	planAdd(t, store, child)
	if _, err := store.Claim("going", "worker-b"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if err := store.StopRoot("the person stopped it"); err != nil {
		t.Fatalf("stop root: %v", err)
	}
	if root := store.Task("root"); root.Status != StatusCancelled || root.Error != "the person stopped it" || root.CompletedAt.IsZero() {
		t.Fatalf("the run's own task after a stop = %s, %q, ended %v", root.Status, root.Error, root.CompletedAt)
	}
	for _, id := range []string{"going", "waiting", "late"} {
		if task := store.Task(id); task.Status != StatusCancelled || task.ClaimedBy != "" {
			t.Fatalf("task %s after the run was stopped = %s, claimed by %q", id, task.Status, task.ClaimedBy)
		}
	}
	if task := store.Task("landed"); task.Status != StatusDone || task.Result != "landed delivered" {
		t.Fatalf("work that had already landed was rewritten by the stop: %s, %q", task.Status, task.Result)
	}
	if ready := store.ReadyLeaves(); len(ready) != 0 {
		t.Fatalf("a stopped run still offers %d tasks to a worker", len(ready))
	}
	// TWO PRESSES ARE ONE STOP, and a run that ended by itself is left as it ended.
	if err := store.StopRoot("again"); err != nil {
		t.Fatalf("a second stop was refused: %v", err)
	}
	if root := store.Task("root"); root.Error != "the person stopped it" {
		t.Fatalf("a second stop rewrote the first one's reason: %q", root.Error)
	}
}

// A RUN WHOSE OWN TASK FAILED IS OVER IN THE STORE. Nothing wrote its ending,
// so it read as running for ever and the next hand-off would have adopted it.
// The runtime's verb fails the run's task with the reason, cancels what is
// still open, writes no result, and leaves what had ended as it ended.
func TestFailRootEndsTheRunWithoutAResult(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, store, planSpec("landed", "Landed"), planSpec("waiting", "Waiting"))
	planFinish(t, store, "landed", "worker", "landed delivered")

	if err := store.FailRoot("senior-dev did not finish: its tests fail"); err != nil {
		t.Fatalf("fail root: %v", err)
	}
	root := store.Task("root")
	if root.Status != StatusFailed || root.Error != "senior-dev did not finish: its tests fail" || root.Result != "" || root.CompletedAt.IsZero() {
		t.Fatalf("the run's own task after it failed = %s, %q, result %q, ended %v", root.Status, root.Error, root.Result, root.CompletedAt)
	}
	if task := store.Task("waiting"); task.Status != StatusCancelled {
		t.Fatalf("open work under a failed run = %s, want cancelled", task.Status)
	}
	if task := store.Task("landed"); task.Status != StatusDone || task.Result != "landed delivered" {
		t.Fatalf("work that had already landed was rewritten: %s, %q", task.Status, task.Result)
	}
	if err := store.FailRoot("again"); err != nil || store.Task("root").Error != "senior-dev did not finish: its tests fail" {
		t.Fatalf("a second ending rewrote the first: %v, %q", err, store.Task("root").Error)
	}
}

// A RUN WHOSE PROCESS WENT AWAY IS ENDED WHEN IT WAS LAST SEEN. The next
// process to find its store open ends it at the instant it names, so the run's
// page does not count the hours nobody was driving it; an instant before the
// run began or after now is held inside what can be true of the run.
func TestFailRootAtEndsTheRunAtTheInstantItNames(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	// The run's clock starts where its own task was made, so every instant
	// below is one the run could have lived through.
	clock := store.Task("root").CreatedAt
	store.now = func() time.Time { return clock }
	planAdd(t, store, planSpec("waiting", "Waiting"))

	lastSeen := clock
	clock = clock.Add(11 * time.Hour)
	if err := store.FailRootAt("codeaf closed while senior-dev was running", lastSeen.Add(29*time.Minute)); err != nil {
		t.Fatalf("fail root at: %v", err)
	}
	ended := store.Task("root")
	if ended.Status != StatusFailed || ended.Error != "codeaf closed while senior-dev was running" {
		t.Fatalf("the run's own task = %s (%q), want failed with the sentence", ended.Status, ended.Error)
	}
	if want := lastSeen.Add(29 * time.Minute); !ended.CompletedAt.Equal(want) || !ended.UpdatedAt.Equal(want) {
		t.Fatalf("the run ended at %v (updated %v), want the instant it was last seen, %v", ended.CompletedAt, ended.UpdatedAt, want)
	}
	if task := store.Task("waiting"); task.Status != StatusCancelled || !task.CompletedAt.Equal(lastSeen.Add(29*time.Minute)) {
		t.Fatalf("open work under the run = %s ended %v, want cancelled with the run", task.Status, task.CompletedAt)
	}

	// Held inside the run's own life: never before it began, never after now.
	early := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	early.now = func() time.Time { return clock }
	began := early.Task("root").CreatedAt
	if err := early.FailRootAt("gone", began.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := early.Task("root").CompletedAt; !got.Equal(began) {
		t.Fatalf("an ending before the run began was written at %v, want its start %v", got, began)
	}
	late := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	late.now = func() time.Time { return clock }
	if err := late.FailRootAt("gone", clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := late.Task("root").CompletedAt; !got.Equal(clock) {
		t.Fatalf("an ending in the future was written at %v, want now %v", got, clock)
	}
}

// The ledger's latest charge is the last moment a run was certainly spending,
// and a ledger with none answers nothing rather than a zero-cost instant.
func TestLastSpendAtIsTheLedgersLatestCharge(t *testing.T) {
	clock := time.Date(2026, time.September, 24, 1, 14, 6, 5e8, time.UTC)
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	store.now = func() time.Time { return clock }
	if got := store.LastSpendAt(); !got.IsZero() {
		t.Fatalf("a ledger with no charge answered %v", got)
	}
	for _, step := range []time.Duration{0, 9 * time.Second, 3 * time.Second} {
		clock = clock.Add(step)
		if err := store.AddSpend("root", "delegate/senior-dev", "work", 0.01, 10, 2); err != nil {
			t.Fatal(err)
		}
	}
	want := time.Date(2026, time.September, 24, 1, 14, 18, 5e8, time.UTC)
	if got := store.LastSpendAt(); !got.Equal(want) {
		t.Fatalf("the latest charge = %v, want %v", got, want)
	}
}
